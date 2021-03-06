package bosh

import (
	"fmt"
	"net"
	"os"

	"github.com/EngineerBetter/control-tower/bosh/internal/gcp"
	"github.com/apparentlymart/go-cidr/cidr"
)

// Delete deletes a bosh director
func (client *GCPClient) Delete(stateFileBytes []byte) ([]byte, error) {
	directorPublicIP, err := client.outputs.Get("DirectorPublicIP")
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve director IP: [%v]", err)
	}

	if err = client.boshCLI.RunAuthenticatedCommand(
		"delete-deployment",
		directorPublicIP,
		client.config.GetDirectorPassword(),
		client.config.GetDirectorCACert(),
		false,
		os.Stdout,
		"--force",
	); err != nil {
		return nil, err
	}

	//TODO(px): pull up this so that we use aws.Store
	store := temporaryStore{
		"state.json": stateFileBytes,
	}
	publicCIDR := client.config.GetPublicCIDR()
	_, pubCIDR, err := net.ParseCIDR(publicCIDR)
	if err != nil {
		return store["state.json"], err
	}
	internalGateway, err := cidr.Host(pubCIDR, 1)
	if err != nil {
		return store["state.json"], err
	}
	directorInternalIP, err := cidr.Host(pubCIDR, 6)
	if err != nil {
		return store["state.json"], err
	}
	credentialsPath, err := client.provider.Attr("credentials_path")
	if err != nil {
		return store["state.json"], err
	}
	network, err := client.outputs.Get("Network")
	if err != nil {
		return store["state.json"], err
	}
	publicSubnetwork, err := client.outputs.Get("PublicSubnetworkName")
	if err != nil {
		return store["state.json"], err
	}
	privateSubnetwork, err := client.outputs.Get("PrivateSubnetworkName")
	if err != nil {
		return store["state.json"], err
	}
	project, err := client.provider.Attr("project")
	if err != nil {
		return store["state.json"], err
	}

	err = client.boshCLI.DeleteEnv(store, gcp.Environment{
		DirectorName:       "bosh",
		ExternalIP:         directorPublicIP,
		GcpCredentialsJSON: credentialsPath,
		InternalCIDR:       client.config.GetPublicCIDR(),
		InternalGW:         internalGateway.String(),
		InternalIP:         directorInternalIP.String(),
		Network:            network,
		PrivateSubnetwork:  privateSubnetwork,
		ProjectID:          project,
		PublicKey:          client.config.GetPublicKey(),
		PublicSubnetwork:   publicSubnetwork,
		Spot:               client.config.IsSpot(),
		Zone:               client.provider.Zone("", ""),
	}, client.config.GetDirectorPassword(), client.config.GetDirectorCert(), client.config.GetDirectorKey(), client.config.GetDirectorCACert(), nil)
	return store["state.json"], err
}
