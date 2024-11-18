//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// awsConfigWithCredentials returns the default AWS config with the given region and static credentials.
func awsConfigWithCredentials(ctx context.Context, kubeClient client.Client, awsRegion string, secretName types.NamespacedName) (aws.Config, error) {
	secret := &corev1.Secret{}
	err := wait.PollUntilContextCancel(ctx, 5*time.Second, true, func(ctx context.Context) (done bool, err error) {
		err = kubeClient.Get(ctx, secretName, secret)
		if err == nil {
			return true, nil
		} else if errors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to get credentials secret %s: %w", secretName.Name, err)
	})
	if err != nil {
		return aws.Config{}, fmt.Errorf("failed to get credentials secret %s: %w", secretName.Name, err)
	}

	keyID := string(secret.Data["aws_access_key_id"])
	secretKey := string(secret.Data["aws_secret_access_key"])

	return config.LoadDefaultConfig(ctx,
		config.WithRegion(awsRegion),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(keyID, secretKey, "")))
}

// getELBTagsFromHostName retrieves the tags associated with an Elastic Load Balancer given its hostname.
// It first finds the ARN of the load balancer matching the hostname and then retrieves its tags.
func getELBTagsFromHostName(t *testing.T, elbClient *elasticloadbalancingv2.Client, hostname string) (map[string]string, error) {
	loadBalancerArn, err := getELBARNFromHostname(t, elbClient, hostname)
	if err != nil {
		return nil, err
	}

	result, err := elbClient.DescribeTags(context.TODO(), &elasticloadbalancingv2.DescribeTagsInput{
		ResourceArns: []string{loadBalancerArn},
	})
	if err != nil {
		return nil, err
	}

	tags := make(map[string]string)
	if len(result.TagDescriptions) > 0 {
		for _, tagDesc := range result.TagDescriptions {
			for _, tag := range tagDesc.Tags {
				tags[*tag.Key] = *tag.Value
			}
		}
	}
	t.Logf("Tags present on %s load balancer: %v", loadBalancerArn, tags)
	return tags, nil
}

// getELBARNFromHostname retrieves the ARN of an Elastic Load Balancer given its hostname.
// It queries all load balancers and find the one matching the given hostname.
func getELBARNFromHostname(t *testing.T, elbClient *elasticloadbalancingv2.Client, hostname string) (string, error) {
	result, err := elbClient.DescribeLoadBalancers(context.TODO(), &elasticloadbalancingv2.DescribeLoadBalancersInput{})
	if err != nil {
		return "", err
	}

	var loadBalancerArn string
	for _, lb := range result.LoadBalancers {
		if *lb.DNSName == hostname {
			loadBalancerArn = *lb.LoadBalancerArn
			t.Logf("LoadBalancer ARN: %s", loadBalancerArn)
			break
		}
	}

	if len(loadBalancerArn) > 0 {
		return loadBalancerArn, nil
	}

	return "", fmt.Errorf("no load balancer found with hostname: %s", hostname)
}
