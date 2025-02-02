// Copyright (C) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package vm

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"

	"github.com/ava-labs/avalanche-cli/pkg/application"
	"github.com/ava-labs/avalanche-cli/pkg/binutils"
	"github.com/ava-labs/avalanche-cli/pkg/constants"
	"github.com/ava-labs/avalanche-cli/pkg/models"
	"github.com/ava-labs/avalanche-cli/pkg/statemachine"
	"github.com/ava-labs/avalanche-cli/pkg/ux"
	"github.com/ava-labs/spacesvm/chain"
	"github.com/ava-labs/subnet-evm/core"
)

const (
	defaultSpacesVMAirdropAmount = "1000000"
	defaultMagic                 = uint64(1)
)

func CreateSpacesVMSubnetConfig(
	app *application.Avalanche,
	subnetName string,
	genesisPath string,
	spacesVMVersion string,
) ([]byte, *models.Sidecar, error) {
	var (
		genesisBytes []byte
		sc           *models.Sidecar
		err          error
	)

	if genesisPath == "" {
		genesisBytes, sc, err = createSpacesVMGenesis(app, subnetName, spacesVMVersion)
		if err != nil {
			return nil, &models.Sidecar{}, err
		}
	} else {
		ux.Logger.PrintToUser("Importing genesis")
		genesisBytes, err = os.ReadFile(genesisPath)
		if err != nil {
			return nil, &models.Sidecar{}, err
		}

		// don't need the direction return value here, as we are not inside a state machine prompting loop
		spacesVMVersion, _, err = getVMVersion(app, "Spaces VM", constants.SpacesVMRepoName, spacesVMVersion, false)
		if err != nil {
			return nil, &models.Sidecar{}, err
		}

		sc = &models.Sidecar{
			Name:      subnetName,
			VM:        models.SpacesVM,
			VMVersion: spacesVMVersion,
			Subnet:    subnetName,
		}
	}

	return genesisBytes, sc, nil
}

func getMagic(app *application.Avalanche) (uint64, statemachine.StateDirection, error) {
	useDefault := fmt.Sprintf("Use default (%d)", defaultMagic)
	useCustom := "Set custom"

	options := []string{useDefault, useCustom, goBackMsg}
	option, err := app.Prompt.CaptureList("Set magic", options)
	if err != nil {
		return 0, statemachine.Stop, err
	}
	if option == goBackMsg {
		return 0, statemachine.Backward, nil
	}
	if option == useDefault {
		return defaultMagic, statemachine.Forward, nil
	}
	magic, err := app.Prompt.CaptureUint64("Custom Magic")
	if err != nil {
		return 0, statemachine.Stop, err
	}
	return magic, statemachine.Forward, nil
}

func getDefaultGenesisValues() (uint64, string, core.GenesisAlloc, error) {
	version, err := binutils.GetLatestReleaseVersion(binutils.GetGithubLatestReleaseURL(
		constants.AvaLabsOrg,
		constants.SpacesVMRepoName,
	))
	if err != nil {
		return 0, "", nil, err
	}
	allocs, err := getDefaultAllocation(defaultSpacesVMAirdropAmount)
	if err != nil {
		return 0, "", nil, err
	}
	return defaultMagic, version, allocs, nil
}

func createSpacesVMGenesis(app *application.Avalanche, subnetName string, spacesVMVersion string) ([]byte, *models.Sidecar, error) {
	ux.Logger.PrintToUser("creating subnet %s", subnetName)

	const (
		genesisState = "genesis"
		magicState   = "magic"
		versionState = "version"
		airdropState = "airdrop"
	)

	var (
		magic     uint64
		allocs    core.GenesisAlloc
		direction statemachine.StateDirection
		version   string
		err       error
	)

	spacesVMState, err := statemachine.NewStateMachine(
		[]string{genesisState, magicState, versionState, airdropState},
	)
	if err != nil {
		return nil, nil, err
	}
	for spacesVMState.Running() {
		switch spacesVMState.CurrentState() {
		case genesisState:
			direction = statemachine.Forward
			var useDefault bool
			useDefault, err = app.Prompt.CaptureYesNo("Use default genesis?")
			if useDefault {
				magic, version, allocs, err = getDefaultGenesisValues()
				if err == nil {
					spacesVMState.Stop()
				}
			}
		case magicState:
			magic, direction, err = getMagic(app)
		case versionState:
			version, direction, err = getVMVersion(app, "Spaces VM", constants.SpacesVMRepoName, spacesVMVersion, true)
		case airdropState:
			allocs, direction, err = getAllocation(app, defaultSpacesVMAirdropAmount, new(big.Int).SetUint64(1), "Amount to airdrop")
		default:
			err = errors.New("invalid creation state")
		}
		if err != nil {
			return nil, nil, err
		}
		spacesVMState.NextState(direction)
	}

	genesis := chain.DefaultGenesis()
	genesis.Magic = magic

	customAllocs := make([]*chain.CustomAllocation, 0, len(allocs))
	for address, account := range allocs {
		alloc := &chain.CustomAllocation{
			Address: address,
			Balance: account.Balance.Uint64(),
		}
		customAllocs = append(customAllocs, alloc)
	}
	genesis.CustomAllocation = customAllocs

	jsonBytes, err := json.MarshalIndent(genesis, "", "    ")
	if err != nil {
		return nil, nil, err
	}

	sc := &models.Sidecar{
		Name:      subnetName,
		VM:        models.SpacesVM,
		VMVersion: version,
		Subnet:    subnetName,
	}

	return jsonBytes, sc, nil
}
