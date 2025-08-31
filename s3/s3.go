package s3

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/sync/errgroup"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	hypertemplates "github.com/herdworks/hypertemplates-go"
)

// Provider Type
type Provider struct {
	Endpoint string
	Bucket string
	AccessKey string
	SecretAccessKey string
}

func (provider *Provider) PutFiles(context context.Context, hyperfiles hypertemplates.HyperFiles, buildPath string, domain string) (error) {
	// initialize the s3 client and upload manager
	client,err := provider.Client(context)
	if err != nil { return err }
	uploader := manager.NewUploader(client)

	taskgroup, context := errgroup.WithContext(context)
	for _, hyperfile := range hyperfiles {
		taskgroup.Go(func() (error) {
			// return provider.PutFile(context, hyperfile, buildPath, domain)
			// initialize the object to be uploaded
			fmt.Printf("Publishing %s to %s\n", hyperfile.SubPath(), hyperfile.Href(domain))
			hyperfile.SetMetadata("httpMetadata.contentType", hyperfile.Mimetype())
			hyperfile.SetMetadata("httpMetadata.cacheControl", "no-cache, no-store, must-revalidate")
			object := &s3.PutObjectInput{
				Bucket: aws.String(provider.Bucket),
				Key:    aws.String(hyperfile.DestinationSubPath("")),
				Body:   hyperfile.File(),
				ContentType: aws.String(hyperfile.Mimetype()),
				Metadata:    hyperfile.GetMetadata(),
			}
			// upload the object
			if _,err := uploader.Upload(context, object); err != nil { return errors.Join(fmt.Errorf("error publishing %s", hyperfile.SubPath()), err) } // discard the result?
			fmt.Printf("Published %s as \"%s\"\n", hyperfile.Href(domain), hyperfile.Mimetype())
			return nil
		})
	}
	if err := taskgroup.Wait(); err != nil { return err }
	return nil
}

func (provider *Provider) PutFile(context context.Context, hyperfile hypertemplates.HyperFile, buildPath string, domain string) error {
	// initialize the s3 client and upload manager
	client,err := provider.Client(context)
	if err != nil { return err }
	uploader := manager.NewUploader(client)

	// initialize the object to be uploaded
	fmt.Printf("Publishing %s to %s\n", hyperfile.SubPath(), hyperfile.Href(domain))
	object := &s3.PutObjectInput{
		Bucket: aws.String(provider.Bucket),
		Key:    aws.String(hyperfile.DestinationSubPath("")),
		Body:   hyperfile.File(),
		ContentType: aws.String(hyperfile.Mimetype()),
		Metadata:    hyperfile.GetMetadata(),
	}

	// upload the object
	if _,err := uploader.Upload(context, object); err != nil { return errors.Join(fmt.Errorf("error publishing %s", hyperfile.SubPath()), err) } // discard the result?
	fmt.Printf("Published %s as \"%s\"\n", hyperfile.Href(domain), hyperfile.Mimetype())
	return nil
}

// =============================================================================
// HELPER METHODS
// =============================================================================
func (provider *Provider) Configure(context context.Context) (aws.Config, error) {
	configuration, err := config.LoadDefaultConfig(
		context,
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			provider.AccessKey,
			provider.SecretAccessKey,
			"", // session token
		)),
		config.WithRegion("auto"),
	)
	if err != nil { return aws.Config{}, err }
	return configuration, nil
}

func (provider *Provider) Client(context context.Context) (client *s3.Client, err error) {
	configuration, err := provider.Configure(context)
	if err != nil { return nil, err } // guard
	if len(provider.Endpoint) == 0 { return s3.NewFromConfig(configuration), nil } // guard
	return s3.NewFromConfig(configuration, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(provider.Endpoint)
		options.UsePathStyle = true
	}), nil
}
