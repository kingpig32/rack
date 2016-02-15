package provider

import (
	"fmt"
	"os"

	"github.com/convox/rack/api/provider/aws"
	"github.com/convox/rack/api/structs"
)

var CurrentProvider Provider

type Provider interface {
	InstancesList() (structs.Instances, error)

	SystemGet() (*structs.System, error)
	SystemSave(system structs.System) error
}

func init() {
	var err error

	switch os.Getenv("PROVIDER") {
	case "aws":
		CurrentProvider, err = aws.NewProvider(os.Getenv("AWS_REGION"), os.Getenv("AWS_ACCESS"), os.Getenv("AWS_SECRET"), os.Getenv("AWS_ENDPOINT"))
	default:
		die(fmt.Errorf("PROVIDER must be one of (aws)"))
	}

	if err != nil {
		die(err)
	}
}

/** package-level functions ************************************************************************/

func InstancesList() (structs.Instances, error) {
	return CurrentProvider.InstancesList()
}

func SystemGet() (*structs.System, error) {
	return CurrentProvider.SystemGet()
}

func SystemSave(system structs.System) error {
	return CurrentProvider.SystemSave(system)
}

/** helpers ****************************************************************************************/

func die(err error) {
	fmt.Fprintf(os.Stderr, "ERROR: %s\n", err)
	os.Exit(1)
}
