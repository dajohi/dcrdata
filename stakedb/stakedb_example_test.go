package stakedb

import (
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"github.com/dcrdata/dcrdata/rpcutils"

	"github.com/dcrdata/dcrdata/semver"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/rpcclient"
)

const (
	host = "localhost:9109"
	user = "jdcrd"
	pass = "jdcrd"
	cert
	disableTLS = true
)

var (
	activeNet = &chaincfg.MainNetParams
)

func DIE_IF_ERR(err error, t *testing.T) {
	if err != nil {
		t.Fatal(err)
	}
}

func TestExampleConnectBlockHash(t *testing.T) {
	nodeClient, _, err := ConnectNodeRPC(host, user, pass, cert, disableTLS)
	DIE_IF_ERR(err, t)

	dbName := "stakedb_test"
	DIE_IF_ERR(os.RemoveAll("./"+dbName), t)

	sDB, err := NewStakeDatabase(nodeClient, activeNet, "stakedb_test")
	DIE_IF_ERR(err, t)
	defer sDB.Close()

	block, blockHash, err := rpcutils.GetBlock(1, nodeClient)
	DIE_IF_ERR(err, t)

	t.Logf("Block: %v (%v)", block.Height(), blockHash)

	_, err = sDB.ConnectBlockHash(blockHash)
	DIE_IF_ERR(err, t)

	dbBlock, dbBlockHash, err := sDB.DBState()
	DIE_IF_ERR(err, t)
	if dbBlock != uint32(block.Height()) {
		t.Errorf("Wrong block height: %d vs %d", dbBlock, block.Height())
	}
	if *dbBlockHash != *blockHash {
		t.Errorf("Block hash mismatch: %s vs %s",
			dbBlockHash.String(), blockHash.String())
	}
}

// ConnectNodeRPC attempts to create a new websocket connection to a dcrd node,
// with the given credentials and optional notification handlers.
func ConnectNodeRPC(host, user, pass, cert string, disableTLS bool) (*rpcclient.Client, semver.Semver, error) {
	var dcrdCerts []byte
	var err error
	var nodeVer semver.Semver
	if !disableTLS {
		dcrdCerts, err = ioutil.ReadFile(cert)
		if err != nil {
			return nil, nodeVer, err
		}

	}

	connCfgDaemon := &rpcclient.ConnConfig{
		Host:         host,
		Endpoint:     "ws", // websocket
		User:         user,
		Pass:         pass,
		Certificates: dcrdCerts,
		DisableTLS:   disableTLS,
	}

	dcrdClient, err := rpcclient.New(connCfgDaemon, nil)
	if err != nil {
		return nil, nodeVer, fmt.Errorf("Failed to start dcrd RPC client: %s", err.Error())
	}

	// Ensure the RPC server has a compatible API version.
	ver, err := dcrdClient.Version()
	if err != nil {
		return nil, nodeVer, fmt.Errorf("unable to get node RPC version")
	}

	dcrdVer := ver["dcrdjsonrpcapi"]
	nodeVer = semver.NewSemver(dcrdVer.Major, dcrdVer.Minor, dcrdVer.Patch)

	return dcrdClient, nodeVer, nil
}
