package email

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ses"

	"launcher/config"
)

var sesClient *ses.Client

func getSESClient() *ses.Client {
	if sesClient != nil {
		return sesClient
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(config.SESRegion),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			config.SESSMTPUser,
			config.SESSMTPPassword,
			"",
		)),
	)
	if err != nil {
		sesClient = nil
		return nil
	}

	sesClient = ses.NewFromConfig(awsCfg)
	return sesClient
}

func SendEmail(to, subject, body string) error {
	ctx := context.Background()

	client := getSESClient()
	if client == nil {
		return fmt.Errorf("SES client not initialized")
	}

	input := &ses.SendEmailInput{
		Source: aws.String(config.SESFromEmail),
		Destination: &ses.Destination{
			ToAddresses: []string{to},
		},
		Message: &ses.Message{
			Subject: &ses.Content{
				Data: aws.String(subject),
			},
			Body: &ses.Body{
				Html: &ses.Content{
					Data: aws.String(body),
				},
			},
		},
	}

	result, err := client.SendEmail(ctx, input)
	if err != nil {
		return fmt.Errorf("SES send email failed: %w", err)
	}

	fmt.Printf("SES email sent successfully: %s\n", *result.MessageId)
	return nil
}
