package uploads3_test

import (
	"context"
	"fmt"
	"strings"
	"testing/fstest"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/ubgo/brick"
	uploads3 "github.com/ubgo/brick/contrib/upload-s3"
)

// uploadTheme is a page with no components: the example is about the sink.
var uploadTheme = fstest.MapFS{
	"content/index.html": {Data: []byte("<h1>Upload</h1>\n")},
}

// The host builds the *s3.Client (region, endpoint, credentials, path
// style) and the sink only uses it. Here the endpoint is a name that is
// never contacted: presigning signs locally, so this example runs with
// no network and no account. Point BaseEndpoint at MinIO or RustFS, or
// drop it for AWS, in a real host.
func Example() {
	client := s3.New(s3.Options{
		Region:       "us-east-1",
		BaseEndpoint: aws.String("http://s3.invalid"),
		UsePathStyle: true,
		Credentials:  credentials.NewStaticCredentialsProvider("example-key", "example-secret", ""),
	})
	sink, err := uploads3.New(client, uploads3.Options{
		Bucket:    "my-app-uploads",
		PublicURL: func(key string) string { return "https://cdn.example.com/" + key },
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	if _, err := brick.New(brick.Options{FS: uploadTheme, Uploads: sink}); err != nil {
		fmt.Println(err)
		return
	}

	up, err := sink.Presign(context.Background(), brick.UploadMeta{Name: "photo.png", Type: "image/png", Size: 2048})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(up.Method)
	fmt.Println(strings.HasPrefix(up.URL, "http://s3.invalid/my-app-uploads/"+uploads3.DefaultTempPrefix))
	fmt.Println(up.Headers["Content-Type"])
	// Output:
	// PUT
	// true
	// image/png
}

// A sink needs a client: a nil one is an error from New, not a panic later.
func ExampleNew_nilClient() {
	_, err := uploads3.New(nil, uploads3.Options{Bucket: "b"})
	fmt.Println(err)
	// Output:
	// uploads3: nil *s3.Client
}
