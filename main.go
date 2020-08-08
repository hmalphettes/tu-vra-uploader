package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	netURL "net/url"
	"os"
	"strings"
	"time"

	"github.com/eventials/go-tus"
	"github.com/spf13/cobra"
)

var (
	rootCmd     *cobra.Command
	bearerToken = os.Getenv("BEARER_TOKEN")
	url         = os.Getenv("URL")
	file        = os.Getenv("FILE")
	vraPassword = os.Getenv("VRA_PASSWORD")
)

func main() {
	rootCmd = &cobra.Command{
		Use:     "tus-uploader",
		Short:   "TUS Uploader client to upload a file on a TUS server",
		Long:    `TUS Uploader streams file to a target URL.`,
		Example: `./tus-uploader --vra-username=admin --vra-password=XXX Infoblox.zip https://vrahost/provisioning/ipam/api/providers/packages/import`,
		RunE:    execute,
	}
	rootCmd.Flags().String("source", "", "path to the file to upload")
	rootCmd.Flags().String("target", "", "url to upload to")
	rootCmd.Flags().StringSlice("header", nil, "Extra headers. eg: Authorization: Bearer")
	rootCmd.Flags().Bool("skip-ssl-verification", false, "Set to true to skip the validation of the TLS certificates")
	rootCmd.Flags().String("vra-username", "", "VRA Username")
	rootCmd.Flags().String("vra-password", "", "VRA Password")
	rootCmd.Flags().Bool("vra-import", false, "VRA Import the bundle")
	rootCmd.Flags().Bool("verbose", true, "When true outputs the vra-token")

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func execute(cmd *cobra.Command, args []string) error {
	file, err := cmd.Flags().GetString("source")
	if err != nil {
		return err
	}
	url, err := cmd.Flags().GetString("target")
	if err != nil {
		return err
	}
	headers, err := cmd.Flags().GetStringSlice("header")
	if err != nil {
		return err
	}
	skipTLSVerification, err := cmd.Flags().GetBool("skip-ssl-verification")
	if err != nil {
		return err
	}
	httpHeaders := make(http.Header)
	for _, header := range headers {
		toks := strings.SplitN(header, ":", 1)
		if len(toks) != 2 {
			return fmt.Errorf("Invalid header value '%s'. It must have a header-name:value separated by a column", headers)
		}
		httpHeaders.Add(toks[0], strings.TrimSpace(toks[1]))
	}
	vraUser, err := cmd.Flags().GetString("vra-username")
	if err != nil {
		return err
	}
	vraPassword, err := cmd.Flags().GetString("vra-password")
	if err != nil {
		return err
	}
	vraImport, err := cmd.Flags().GetBool("vra-import")
	if err != nil {
		return err
	}

	if vraUser != "" {
		vraImport = true
	}

	for _, arg := range args {
		if file == "" {
			file = arg
		} else if url == "" {
			url = arg
		}
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}

	defer f.Close()

	fmt.Printf("TUS Uploading %s to %s\n", file, url)

	// create the tus client.
	clientConfig := tus.DefaultConfig()
	clientConfig.Header = httpHeaders
	if skipTLSVerification {
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		clientConfig.HttpClient = &http.Client{Transport: tr}
	}
	client, err := tus.NewClient(url, clientConfig)
	if err != nil {
		return err
	}

	if bearerToken != "" {
		httpHeaders.Set("Authorization", "Bearer "+bearerToken)
	} else if vraUser != "" {
		vraToken, err := vraToken(vraUser, vraPassword, client, clientConfig)
		if err != nil {
			return err
		}
		bearerToken = vraToken
		httpHeaders.Set("Authorization", "Bearer "+bearerToken)
	}

	// (Optional) Create a chan to notify upload status
	uploadChan := make(chan tus.Upload, 1)
	go func() {
		for uploadStatus := range uploadChan {
			// Print the upload status
			fmt.Printf("%s Completed %v%% %v Bytes of %v Bytes\n",
				time.Now().Format("2006-01-02 15:04:05"),
				uploadStatus.Progress(),
				uploadStatus.Offset(),
				uploadStatus.Size())
		}
	}()

	// create an upload from a file.
	upload, err := tus.NewUploadFromFile(f)
	if err != nil {
		return err
	}

	var uploader *tus.Uploader

	// Declare number of attempts
	const attemps = 50
	for i := 1; i <= attemps; i++ {
		if i > 1 {
			fmt.Printf("%s Attemp %v of %v\n", time.Now().Format("2006-01-02 15:04:05"), i, attemps)
		}
		// Create an uploader
		uploader, err = client.CreateOrResumeUpload(upload)
		if err != nil {
			if i == 1 { // on the first error, see if the problem is recoverable or not
				errMsg := err.Error()
				if strings.Contains(errMsg, "403") || strings.Contains(errMsg, "401") || strings.Contains(errMsg, "404") || strings.Contains(errMsg, "400") {
					break // Unrecoverable error
				}
			}
			fmt.Println("Error", err)
			fmt.Println("Trying again in 10 seconds")
			time.Sleep(time.Second * 10)
			continue
		}
		if i == 1 {
			fmt.Printf("%s Starting the upload to %s\n", time.Now().Format("2006-01-02 15:04:05"), uploader.Url())
		}
		// (Optional) Notify Upload Status
		uploader.NotifyUploadProgress(uploadChan)
		// Start upload to server
		err = uploader.Upload()
		if err != nil {
			fmt.Println("Error", err)
			fmt.Println("Trying again in 10 seconds")
			time.Sleep(time.Second * 10)
			continue
		}
		break
	}

	if err != nil {
		return err
	}
	fmt.Printf("%s Done uploading\n", time.Now().Format("2006-01-02 15:04:05"))

	if vraImport && bearerToken != "" {
		err = vraImportBundle(bearerToken, client, uploader, clientConfig)
	}

	return nil
}

func vraToken(username, password string, client *tus.Client, clientConfig *tus.Config) (string, error) {
	cspLoginPath := "/csp/gateway/am/api/login?access_token"

	baseURL, err := netURL.Parse(client.Url)
	if err != nil {
		return "", err
	}

	url := baseURL.Scheme + "://" + baseURL.Host + cspLoginPath
	payload, err := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})
	if err != nil {
		return "", err
	}
	response, err := clientConfig.HttpClient.Post(url, "application/json", bytes.NewBuffer(payload))
	if err != nil {
		return "", err
	}

	defer response.Body.Close()

	// fmt.Println("response Status:", response.Status)
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return "", err
	}
	if response.StatusCode != 200 {
		return "", fmt.Errorf("Failed to login on %s: %s", url, string(body))
	}
	respAsMap := make(map[string]interface{})
	err = json.Unmarshal(body, &respAsMap)
	if err != nil {
		return "", err
	}
	// fmt.Println("response Body:", string(body))
	// fmt.Printf("ahaha %+v\n", respAsMap)
	return respAsMap["access_token"].(string), nil
}

func vraImportBundle(bearerToken string, client *tus.Client, uploader *tus.Uploader, clientConfig *tus.Config) error {
	toks := strings.Split(uploader.Url(), "/")
	bundleID := toks[len(toks)-1]

	payload, err := json.Marshal(map[string]string{
		"bundleId": bundleID,
		"option":   "OVERWRITE",
	})
	if err != nil {
		return err
	}
	fmt.Printf("Importing the bundle in VRA %s/%s\n", client.Url, bundleID)

	request, err := http.NewRequest("POST", client.Url, bytes.NewBuffer(payload))
	request.Header.Set("Authorization", "Bearer "+bearerToken)
	request.Header.Set("Content-Type", "application/json")

	response, err := clientConfig.HttpClient.Do(request)
	if err != nil {
		return err
	}

	defer response.Body.Close()

	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return err
	}
	respAsMap := make(map[string]interface{})
	err = json.Unmarshal(body, &respAsMap)
	if err != nil {
		return err
	}

	if response.StatusCode == 201 {
		fmt.Printf("Bundle imported into VRA: %s %s\n", respAsMap["providerName"].(string), respAsMap["providerVersion"].(string))
		return nil
	}

	fmt.Println("response Status:", response.Status)
	fmt.Println("response Headers:", response.Header)
	fmt.Println("response Body:", string(body))

	return fmt.Errorf("Failed to import the bundle. StatusCode was '%s' instead of 200/OK", response.Status)
}
