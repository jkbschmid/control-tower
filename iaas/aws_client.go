package iaas

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/route53"
)

// AWSClient is the concrete implementation of IClient on AWS
type AWSClient struct {
	sess *session.Session
}

// IEC2 only implements functions used in the iaas package
type IEC2 interface {
	DescribeSecurityGroups(input *ec2.DescribeSecurityGroupsInput) (*ec2.DescribeSecurityGroupsOutput, error)
	DescribeVolumes(input *ec2.DescribeVolumesInput) (*ec2.DescribeVolumesOutput, error)
	DeleteVolume(input *ec2.DeleteVolumeInput) (*ec2.DeleteVolumeOutput, error)
}

func newAWS(region string) (IClient, error) {
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(region),
	})
	if err != nil {
		return nil, err
	}
	return &AWSClient{sess}, nil
}

// Region returns the region to operate against
func (client *AWSClient) Region() string {
	return *client.sess.Config.Region
}

// IAAS returns the iaas to operate against
func (client *AWSClient) IAAS() string {
	return "AWS"
}

// CheckForWhitelistedIP checks if the specified IP is whitelisted in the security group
func (client *AWSClient) CheckForWhitelistedIP(ip, securityGroup string) (bool, error) {

	cidr := fmt.Sprintf("%s/32", ip)

	ec2Client := ec2.New(client.sess)

	securityGroupsOutput, err := ec2Client.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
		GroupIds: []*string{
			aws.String(securityGroup),
		},
	})
	if err != nil {
		return false, err
	}

	ingressPermissions := securityGroupsOutput.SecurityGroups[0].IpPermissions

	port22, port6868, port25555 := false, false, false
	for _, entry := range ingressPermissions {
		for _, sgIP := range entry.IpRanges {
			checkPorts(*sgIP.CidrIp, cidr, &port22, &port6868, &port25555, *entry.FromPort)
		}
	}

	if port22 && port6868 && port25555 {
		return true, nil
	}

	return false, nil
}

func checkPorts(sgCidr, cidr string, port22, port6868, port25555 *bool, fromPort int64) {
	if sgCidr == cidr {
		switch fromPort {
		case 22:
			*port22 = true
		case 6868:
			*port6868 = true
		case 25555:
			*port25555 = true
		}
	}
}

// DeleteVolumes deletes the specified EBS volumes
func (client *AWSClient) DeleteVolumes(volumes []*string, deleteVolume func(ec2Client IEC2, volumeID *string) error) error {
	if len(volumes) == 0 {
		return nil
	}

	ec2Client := ec2.New(client.sess)

	volumesOutput, err := ec2Client.DescribeVolumes(&ec2.DescribeVolumesInput{
		Filters: []*ec2.Filter{
			{
				Name: aws.String("status"),
				Values: []*string{
					aws.String("available"),
				},
			},
			{
				Name:   aws.String("volume-id"),
				Values: volumes,
			},
		},
	})

	if err != nil {
		return err
	}

	volumesToDelete := volumesOutput.Volumes

	for _, volume := range volumesToDelete {
		volumeID := volume.VolumeId
		err = deleteVolume(ec2Client, volumeID)
		if err != nil {
			return err
		}
	}
	return nil
}

// DeleteVolume deletes an EBS volume with the given ID
func DeleteVolume(ec2Client IEC2, volumeID *string) error {
	fmt.Printf("Deleting volume: %s\n", *volumeID)
	_, err := ec2Client.DeleteVolume(&ec2.DeleteVolumeInput{
		VolumeId: volumeID,
	})
	return err
}

// DeleteVMsInVPC deletes all the VMs in the given VPC
func (client *AWSClient) DeleteVMsInVPC(vpcID string) ([]*string, error) {

	filterName := "vpc-id"
	ec2Client := ec2.New(client.sess)

	resp, err := ec2Client.DescribeInstances(&ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			&ec2.Filter{
				Name: &filterName,
				Values: []*string{
					&vpcID,
				},
			},
		},
	})
	if err != nil {
		return nil, err
	}

	instancesToTerminate := []*string{}
	volumesToDelete := []*string{}
	for _, reservation := range resp.Reservations {
		for _, instance := range reservation.Instances {
			fmt.Printf("Terminating instance %s\n", *instance.InstanceId)
			instancesToTerminate = append(instancesToTerminate, instance.InstanceId)
			for _, blockDevice := range instance.BlockDeviceMappings {
				volumesToDelete = append(volumesToDelete, blockDevice.Ebs.VolumeId)
			}
		}
	}

	if len(instancesToTerminate) == 0 {
		return nil, nil
	}

	_, err = ec2Client.TerminateInstances(&ec2.TerminateInstancesInput{
		InstanceIds: instancesToTerminate,
	})
	if err != nil {
		return nil, err
	}

	return volumesToDelete, nil
}

// ListHostedZones returns a list of hosted zones
func (client *AWSClient) ListHostedZones() ([]*route53.HostedZone, error) {

	r53Client := route53.New(client.sess)
	hostedZones := []*route53.HostedZone{}
	err := r53Client.ListHostedZonesPages(&route53.ListHostedZonesInput{}, func(output *route53.ListHostedZonesOutput, _ bool) bool {
		hostedZones = append(hostedZones, output.HostedZones...)
		return true
	})
	if err != nil {
		return nil, err
	}

	return hostedZones, nil
}

// FindLongestMatchingHostedZone finds the longest hosted zone that matches the given subdomain
func (client *AWSClient) FindLongestMatchingHostedZone(subdomain string) (string, string, error) {
	hostedZones, err := client.ListHostedZones()
	if err != nil {
		return "", "", err
	}

	longestMatchingHostedZoneName := ""
	longestMatchingHostedZoneID := ""
	for _, hostedZone := range hostedZones {
		domain := strings.TrimRight(*hostedZone.Name, ".")
		id := *hostedZone.Id
		if strings.HasSuffix(subdomain, domain) {
			if len(domain) > len(longestMatchingHostedZoneName) {
				longestMatchingHostedZoneName = domain
				longestMatchingHostedZoneID = id
			}
		}
	}

	if longestMatchingHostedZoneName == "" {
		return "", "", fmt.Errorf("No matching hosted zone found for domain %s", subdomain)
	}

	longestMatchingHostedZoneID = strings.Replace(longestMatchingHostedZoneID, "/hostedzone/", "", -1)

	return longestMatchingHostedZoneName, longestMatchingHostedZoneID, err
}
