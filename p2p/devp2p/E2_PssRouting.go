// pss send-to-self hello world
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/swarm"
	bzzapi "github.com/ethereum/go-ethereum/swarm/api"
	"github.com/ethereum/go-ethereum/swarm/pss"

	demo "./common"
)

func newService(bzzdir string, bzzport int, bzznetworkid uint64) func(ctx *node.ServiceContext) (node.Service, error) {
	return func(ctx *node.ServiceContext) (node.Service, error) {

		// generate a new private key
		privkey, err := crypto.GenerateKey()
		if err != nil {
			demo.Log.Crit("private key generate servicenode 'left' fail: %v")
		}

		// create necessary swarm params
		bzzconfig := bzzapi.NewConfig()
		bzzconfig.Path = bzzdir
		bzzconfig.Init(privkey)
		if err != nil {
			demo.Log.Crit("unable to configure swarm", "err", err)
		}
		bzzconfig.Port = fmt.Sprintf("%d", bzzport)

		// shortcut to setting up a swarm node
		return swarm.NewSwarm(bzzconfig, nil)

	}
}

func main() {

	// create three nodes
	l_stack, err := demo.NewServiceNode(demo.P2pPort, 0, 0)
	if err != nil {
		demo.Log.Crit(err.Error())
	}
	r_stack, err := demo.NewServiceNode(demo.P2pPort+1, 0, 0)
	if err != nil {
		demo.Log.Crit(err.Error())
	}
	c_stack, err := demo.NewServiceNode(demo.P2pPort+2, 0, 0)
	if err != nil {
		demo.Log.Crit(err.Error())
	}

	// register the pss activated bzz services
	l_svc := newService(l_stack.InstanceDir(), demo.BzzDefaultPort, demo.BzzDefaultNetworkId)
	err = l_stack.Register(l_svc)
	if err != nil {
		demo.Log.Crit("servicenode 'left' pss register fail", "err", err)
	}
	r_svc := newService(r_stack.InstanceDir(), demo.BzzDefaultPort+1, demo.BzzDefaultNetworkId)
	err = r_stack.Register(r_svc)
	if err != nil {
		demo.Log.Crit("servicenode 'right' pss register fail", "err", err)
	}
	c_svc := newService(c_stack.InstanceDir(), demo.BzzDefaultPort+2, demo.BzzDefaultNetworkId)
	err = c_stack.Register(c_svc)
	if err != nil {
		demo.Log.Crit("servicenode 'right' pss register fail", "err", err)
	}

	// start the nodes
	err = l_stack.Start()
	if err != nil {
		demo.Log.Crit("servicenode start failed", "err", err)
	}
	defer os.RemoveAll(l_stack.DataDir())
	err = r_stack.Start()
	if err != nil {
		demo.Log.Crit("servicenode start failed", "err", err)
	}
	defer os.RemoveAll(r_stack.DataDir())
	err = c_stack.Start()
	if err != nil {
		demo.Log.Crit("servicenode start failed", "err", err)
	}
	defer os.RemoveAll(c_stack.DataDir())

	// connect the nodes to the middle
	c_stack.Server().AddPeer(l_stack.Server().Self())
	c_stack.Server().AddPeer(r_stack.Server().Self())

	// get the rpc clients
	l_rpcclient, err := l_stack.Attach()
	r_rpcclient, err := r_stack.Attach()

	// wait until the state of the swarm overlay network is ready
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = demo.WaitHealthy(ctx, 2, l_rpcclient, r_rpcclient)
	if err != nil {
		demo.Log.Crit("health check fail", "err", err)
	}
	time.Sleep(time.Second) // because the healthy does not work

	// get a valid topic byte
	var topic string
	err = l_rpcclient.Call(&topic, "pss_stringToTopic", "foo")
	if err != nil {
		demo.Log.Crit("pss string to topic fail", "err", err)
	}

	// subscribe to incoming messages on the receiving sevicenode
	// this will register a message handler on the specified topic
	msgC := make(chan pss.APIMsg)
	sub, err := r_rpcclient.Subscribe(context.Background(), "pss", msgC, "receive", topic, false, false)

	// supply no address for routing
	r_bzzaddr := "0x"

	// get the receiver's public key
	var r_pubkey string
	err = r_rpcclient.Call(&r_pubkey, "pss_getPublicKey")
	if err != nil {
		demo.Log.Crit("pss get pubkey fail", "err", err)
	}

	// make the sender aware of the receiver's public key
	err = l_rpcclient.Call(nil, "pss_setPeerPublicKey", r_pubkey, topic, r_bzzaddr)
	if err != nil {
		demo.Log.Crit("pss get pubkey fail", "err", err)
	}

	// send message using asymmetric encryption
	// since it's sent to ourselves, it will not go through pss forwarding
	err = l_rpcclient.Call(nil, "pss_sendAsym", r_pubkey, topic, common.ToHex([]byte("bar")))
	if err != nil {
		demo.Log.Crit("pss send fail", "err", err)
	}

	// get the incoming message
	inmsg := <-msgC
	demo.Log.Info("pss received", "msg", string(inmsg.Msg), "from", fmt.Sprintf("%x", inmsg.Key))

	// bring down the servicenodes
	sub.Unsubscribe()
	r_rpcclient.Close()
	l_rpcclient.Close()
	c_stack.Stop()
	r_stack.Stop()
	l_stack.Stop()
}
