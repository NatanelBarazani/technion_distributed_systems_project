package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"dp_project/server"
)

func main() {
	nodeID := os.Getenv("NODE_ID")
	if nodeID == "" {
		nodeID = "server1"
	}
	etcdEndpoints := os.Getenv("ETCD_ENDPOINTS")
	if etcdEndpoints == "" {
		etcdEndpoints = "localhost:2379"
	}
	grpcPort := 50051
	if val := os.Getenv("GRPC_PORT"); val != "" {
		if p, err := strconv.Atoi(val); err == nil {
			grpcPort = p
		}
	}
	restPort := 8081
	if val := os.Getenv("REST_PORT"); val != "" {
		if p, err := strconv.Atoi(val); err == nil {
			restPort = p
		}
	}

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{etcdEndpoints},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		panic(err)
	}
	defer cli.Close()

	fmt.Printf("[Init] Node %s. etcd Endpoints: %s, gRPC port: %d, REST port: %d\n",
		nodeID, etcdEndpoints, grpcPort, restPort)

	node, err := server.NewNode(nodeID, cli, grpcPort, restPort)
	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go node.RunHeartbeats(ctx)
	go node.RunLeaderElection(ctx)
	go node.RunPaxosRPCServer()
	go node.RunRESTServer()
	go node.RunSnapshotting(ctx) // Start periodic snapshotting

	select {}
}
