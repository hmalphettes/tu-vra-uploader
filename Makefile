build:
	GOOS=windows go build -mod=vendor -o tus-vra-uploader.exe main.go
	GOOS=linux go build -mod=vendor -o tus-vra-uploader main.go
	GOOS=darwin go build -mod=vendor -o tus-vra-uploader-darwin main.go
