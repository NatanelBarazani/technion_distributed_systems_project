package server

import (
	"context"
	"fmt"
	"strconv"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
)

type Node struct {
	ID          string
	EtcdCli     *clientv3.Client
	GrpcPort    int
	RestPort    int
	session     *concurrency.Session
	election    *concurrency.Election
	PaxosEngine *MultiPaxos
	isLeader    bool
}

func NewNode(id string, cli *clientv3.Client, grpcPort, restPort int) (*Node, error) {
	sess, err := concurrency.NewSession(cli, concurrency.WithTTL(5))
	if err != nil {
		return nil, err
	}
	elect := concurrency.NewElection(sess, "/my-election/")
	mp := NewMultiPaxos(id)
	return &Node{
		ID:          id,
		EtcdCli:     cli,
		GrpcPort:    grpcPort,
		RestPort:    restPort,
		session:     sess,
		election:    elect,
		PaxosEngine: mp,
	}, nil
}

func (n *Node) RunHeartbeats(ctx context.Context) {
	leaseTTL := int64(5)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		leaseResp, err := n.EtcdCli.Grant(ctx, leaseTTL)
		if err != nil {
			fmt.Printf("Node %s: error creating lease: %v\n", n.ID, err)
			time.Sleep(1 * time.Second)
			continue
		}
		_, err = n.EtcdCli.Put(ctx, "/nodes/"+n.ID+"/heartbeat", "alive", clientv3.WithLease(leaseResp.ID))
		if err != nil {
			fmt.Printf("Node %s: error putting heartbeat: %v\n", n.ID, err)
			time.Sleep(1 * time.Second)
			continue
		}
		kaCh, err := n.EtcdCli.KeepAlive(ctx, leaseResp.ID)
		if err != nil {
			fmt.Printf("Node %s: KeepAlive error: %v\n", n.ID, err)
			time.Sleep(1 * time.Second)
			continue
		}
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-kaCh:
				if !ok {
					break
				}
				time.Sleep(1 * time.Second)
			}
		}
	}
}

func (n *Node) RunLeaderElection(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		fmt.Printf("Node %s campaigning...\n", n.ID)
		err := n.election.Campaign(ctx, n.ID)
		if err != nil {
			fmt.Printf("Node %s: campaign error: %v\n", n.ID, err)
			time.Sleep(1 * time.Second)
			continue
		}

		n.isLeader = true
		fmt.Printf("Node %s is leader!\n", n.ID)

		// Write the leader's REST URL into etcd so that non-leader nodes can learn it.
		if _, err := n.EtcdCli.Put(context.Background(), "/currentLeader", n.GetLocalRESTURL()); err != nil {
			fmt.Printf("Error setting current leader in etcd: %v\n", err)
		}

		select {
		case <-n.session.Done():
			n.isLeader = false
			fmt.Printf("Node %s lost leadership.\n", n.ID)
			time.Sleep(1 * time.Second)
			sess, _ := concurrency.NewSession(n.EtcdCli, concurrency.WithTTL(5))
			n.session = sess
			n.election = concurrency.NewElection(sess, "/my-election/")
		case <-ctx.Done():
			return
		}
	}
}


func (n *Node) RunPaxosRPCServer() {
	// Placeholder for gRPC server implementation.
}

func (n *Node) RunRESTServer() {
	StartHTTPServer(n.RestPort, n)
}

func (n *Node) IsLeader() bool {
	return n.isLeader
}

func (n *Node) GetLeaderRESTURL() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := n.EtcdCli.Get(ctx, "/currentLeader")
	if err != nil || len(resp.Kvs) == 0 {
		// If there’s an error or no leader is set, fallback to this node’s URL.
		return n.GetLocalRESTURL()
	}
	return string(resp.Kvs[0].Value)
}
func (n *Node) GetLocalRESTURL() string {
	// Assuming your docker-compose service names match your NODE_ID and that
	// containers can resolve each other by these names.
	return "http://" + n.ID + ":" + strconv.Itoa(n.RestPort)
}

func (n *Node) GetPaxosEngine() *MultiPaxos {
	return n.PaxosEngine
}
