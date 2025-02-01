package server

import (
	"context"
	"fmt"
	"strconv"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
)

// Node represents a server node in the replicated log system.
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

// NewNode creates a new Node instance. It attempts to load a snapshot from etcd.
func NewNode(id string, cli *clientv3.Client, grpcPort, restPort int) (*Node, error) {
	sess, err := concurrency.NewSession(cli, concurrency.WithTTL(5))
	if err != nil {
		return nil, err
	}
	elect := concurrency.NewElection(sess, "/my-election/")
	mp := NewMultiPaxos(id)

	// Attempt to load snapshot from etcd under key "/snapshot".
	if err := mp.LoadSnapshotFromEtcd(cli, "/snapshot"); err != nil {
		fmt.Printf("Node %s: Error loading snapshot from etcd: %v\n", id, err)
	}

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

// RunHeartbeats sends periodic heartbeats to etcd.
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

// RunLeaderElection continuously campaigns for leadership.
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
		// When elected leader, write the leader's REST URL into etcd.
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

// RunPaxosRPCServer is a placeholder for the internal gRPC server implementation.
func (n *Node) RunPaxosRPCServer() {
	// In a full implementation, start your gRPC server here.
}

// RunRESTServer starts the REST API server.
func (n *Node) RunRESTServer() {
	StartHTTPServer(n.RestPort, n)
}

// IsLeader returns true if this node is the current leader.
func (n *Node) IsLeader() bool {
	return n.isLeader
}

// GetLeaderRESTURL queries etcd for the current leader's REST URL.
func (n *Node) GetLeaderRESTURL() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := n.EtcdCli.Get(ctx, "/currentLeader")
	if err != nil || len(resp.Kvs) == 0 {
		return n.GetLocalRESTURL()
	}
	return string(resp.Kvs[0].Value)
}

// GetLocalRESTURL returns this node's REST URL.
// For Docker, we assume NODE_ID is the container name.
func (n *Node) GetLocalRESTURL() string {
	return "http://" + n.ID + ":" + strconv.Itoa(n.RestPort)
}

// GetPaxosEngine returns the node's MultiPaxos instance.
func (n *Node) GetPaxosEngine() *MultiPaxos {
	return n.PaxosEngine
}

// RunSnapshotting periodically takes a snapshot and stores it in etcd.
func (n *Node) RunSnapshotting(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second) // adjust the interval as needed
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			err := n.PaxosEngine.SaveSnapshotToEtcd(n.EtcdCli, "/snapshot")
			if err != nil {
				fmt.Printf("Error saving snapshot to etcd: %v\n", err)
			} else {
				fmt.Printf("Snapshot saved to etcd successfully.\n")
			}
		}
	}
}
