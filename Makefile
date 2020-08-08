build:
	GOOS=windows go build -mod=vendor -o tus-vra-uploader.exe main.go
	GOOS=linux go build -mod=vendor -o tus-vra-uploader-linux64 main.go
	GOOS=darwin go build -mod=vendor -o tus-vra-uploader-darwin main.go

compress-build: build
	upx tus-vra-uploader-linux64
	upx tus-vra-uploader.exe
	upx tus-vra-uploader-darwin