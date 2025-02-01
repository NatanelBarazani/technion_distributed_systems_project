package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
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

	// KnownNodes maps node IDs to their REST URLs for replication.
	KnownNodes map[string]string
}

// NewNode creates a new Node instance and loads any snapshot from etcd.
func NewNode(id string, cli *clientv3.Client, grpcPort, restPort int) (*Node, error) {
	sess, err := concurrency.NewSession(cli, concurrency.WithTTL(5))
	if err != nil {
		return nil, err
	}
	elect := concurrency.NewElection(sess, "/my-election/")
	mp := NewMultiPaxos(id)
	// Load snapshot from etcd (if available) under key "/snapshot".
	if err := mp.LoadSnapshotFromEtcd(cli, "/snapshot"); err != nil {
		fmt.Printf("Node %s: Error loading snapshot from etcd: %v\n", id, err)
	}

	node := &Node{
		ID:          id,
		EtcdCli:     cli,
		GrpcPort:    grpcPort,
		RestPort:    restPort,
		session:     sess,
		election:    elect,
		PaxosEngine: mp,
		KnownNodes:  make(map[string]string),
	}
	node.registerSelf() // register this node's REST URL in membership.
	go node.periodicallyRefreshMembership()
	return node, nil
}

// registerSelf writes this node's REST URL into etcd membership info.
func (n *Node) registerSelf() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := n.EtcdCli.Get(ctx, "/nodes")
	members := make(map[string]string)
	if err == nil && len(resp.Kvs) > 0 {
		json.Unmarshal(resp.Kvs[0].Value, &members)
	}
	members[n.ID] = n.GetLocalRESTURL()
	data, _ := json.Marshal(members)
	_, err = n.EtcdCli.Put(ctx, "/nodes", string(data))
	if err != nil {
		fmt.Printf("Node %s: Error registering membership: %v\n", n.ID, err)
	}
}

// periodicallyRefreshMembership refreshes membership info from etcd.
func (n *Node) periodicallyRefreshMembership() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		resp, err := n.EtcdCli.Get(ctx, "/nodes")
		cancel()
		if err == nil && len(resp.Kvs) > 0 {
			var members map[string]string
			if err := json.Unmarshal(resp.Kvs[0].Value, &members); err == nil {
				n.KnownNodes = members
			}
		}
	}
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
		// When elected, register self and write leader REST URL.
		n.registerSelf()
		if _, err := n.EtcdCli.Put(context.Background(), "/currentLeader", n.GetLocalRESTURL()); err != nil {
			fmt.Printf("Error setting current leader in etcd: %v\n", err)
		}
		// Leader should replicate decisions to followers as they occur.
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

// RunPaxosRPCServer is a placeholder for the gRPC server.
func (n *Node) RunPaxosRPCServer() {
	// In a complete system, start your gRPC server here.
}

// RunRESTServer starts the REST API server.
func (n *Node) RunRESTServer() {
	StartHTTPServer(n.RestPort, n)
}

// RunSnapshotting periodically takes a snapshot and stores it in etcd.
func (n *Node) RunSnapshotting(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second) // adjust interval as needed
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

// IsLeader returns true if this node is the leader.
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
func (n *Node) GetLocalRESTURL() string {
	return "http://" + n.ID + ":" + strconv.Itoa(n.RestPort)
}

// GetPaxosEngine returns the node's MultiPaxos instance.
func (n *Node) GetPaxosEngine() *MultiPaxos {
	return n.PaxosEngine
}

// ReplicateLogEntry sends the decided log entry (instance ID and command)
// to all known nodes except itself via HTTP POST to the /replicate endpoint.
func (n *Node) ReplicateLogEntry(instanceID int, command string) {
	for nodeID, url := range n.KnownNodes {
		if nodeID == n.ID {
			continue
		}
		replicationURL := url + "/replicate"
		payload := map[string]interface{}{
			"instance_id": instanceID,
			"command":     command,
		}
		data, _ := json.Marshal(payload)
		go func(url string, data []byte) {
			resp, err := http.Post(url, "application/json", strings.NewReader(string(data)))
			if err != nil {
				fmt.Printf("Error replicating to %s: %v\n", url, err)
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				body, _ := ioutil.ReadAll(resp.Body)
				fmt.Printf("Replication to %s failed: %s\n", url, string(body))
			}
		}(replicationURL, data)
	}
}

// OnDecision is called by the leader after deciding a log entry.
// It replicates the decision to all followers.
func (n *Node) OnDecision(instanceID int, command string) {
	n.ReplicateLogEntry(instanceID, command)
}
