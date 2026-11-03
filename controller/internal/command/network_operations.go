package command

import (
	"fmt"
	"strconv"

	"dropcheck/controller/internal/controlpb"
)

func NetworksOperation() Operation {
	return NewOperation("network.list", &controlpb.RunCommand{Command: &controlpb.RunCommand_GetNetworks{GetNetworks: &controlpb.GetNetworks{}}}, Options{})
}

func NetworkOperation(target string) (Operation, error) {
	query := &controlpb.GetNetwork{DefaultNetwork: target == "default"}
	if !query.DefaultNetwork {
		if !ValidNetworkID(target) {
			return Operation{}, fmt.Errorf("expected default or a canonical positive Android Network ID")
		}
		query.NetworkId = target
	}
	return NewOperation("network.show", &controlpb.RunCommand{Command: &controlpb.RunCommand_GetNetwork{GetNetwork: query}}, Options{Detail: true}), nil
}

func ValidNetworkID(value string) bool {
	id, err := strconv.ParseUint(value, 10, 31)
	return err == nil && id > 0 && strconv.FormatUint(id, 10) == value
}

func PermissionsOperation() Operation {
	return NewOperation("permissions", &controlpb.RunCommand{Command: &controlpb.RunCommand_GetPermissions{GetPermissions: &controlpb.GetPermissions{}}}, Options{})
}
