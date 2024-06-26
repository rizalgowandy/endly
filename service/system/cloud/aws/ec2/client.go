package ec2

import (
	"fmt"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/viant/endly"
	"github.com/viant/endly/service/system/cloud/aws"
)

var clientKey = (*ec2.EC2)(nil)

func setClient(context *endly.Context, rawRequest map[string]interface{}) error {
	config, err := aws.InitCredentials(context, rawRequest, clientKey)
	if err != nil || config == nil {
		return err
	}
	sess := session.Must(session.NewSession())
	client := ec2.New(sess, config)
	return context.Put(clientKey, client)
}

func getClient(context *endly.Context) (interface{}, error) {
	client := &ec2.EC2{}
	if !context.Contains(clientKey) {
		_ = setClient(context, map[string]interface{}{"client": 1})
	}
	if !context.GetInto(clientKey, &client) {
		return nil, fmt.Errorf("unable to locate client %T, please add Credentials atribute ", client)
	}
	return client, nil
}

// GetClient returns ec2 client from context
func GetClient(context *endly.Context) (*ec2.EC2, error) {
	client, err := getClient(context)
	if err != nil {
		return nil, err
	}
	ec2Client, ok := client.(*ec2.EC2)
	if !ok {
		return nil, fmt.Errorf("unexpected client type: %T", client)
	}

	return ec2Client, nil
}
