package email

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/aws/aws-sdk-go-v2/service/ses/types"

	"launcher/config"
)

var sesClient *ses.Client

func getSESClient() *ses.Client {
	if sesClient != nil {
		return sesClient
	}

	awsCfg := aws.Config{
		Region: config.SESRegion,
		Credentials: credentials.NewStaticCredentialsProvider(
			config.SESSMTPUser,
			config.SESSMTPPassword,
			"",
		),
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
		Destination: &types.Destination{
			ToAddresses: []string{to},
		},
		Message: &types.Message{
			Subject: &types.Content{
				Data: aws.String(subject),
			},
			Body: &types.Body{
				Html: &types.Content{
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
