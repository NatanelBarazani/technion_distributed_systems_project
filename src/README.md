# Distributed Key-Value Store with MultiPaxos Consensus

## Overview

This project implements a **state machine replication** system using a **MultiPaxos-based consensus** algorithm. 
It provides a **fault-tolerant distributed key-value store** that supports linearizable updates and sequentially 
consistent reads.

### **Key Features**
- **MultiPaxos Consensus** for strong consistency.
- **Log Replication** ensures data consistency across nodes.
- **Snapshot Mechanism** for efficient log compaction and recovery.
- **RESTful API** for client interaction.
- **gRPC for Inter-Node Communication**.
- **Etcd Integration** for failure detection & leader election.
- **Dockerized Deployment** for scalability.

## **System Architecture**

The system follows a **distributed leader-follower model** where a leader is elected dynamically using etcd.
- **Clients** send requests to **any node** in the system.
- The **leader** handles write operations & replicates decisions.
- **Followers** apply the log entries & serve read requests.

## **Project Structure**

```
proj.zip
│── report.pdf ........................ Detailed system report
│── students.json ..................... Student info
│── repo.url .......................... GitHub/GitLab repository link
│── src/
│   │── docker/ ........................ Docker-related files
│   │── server/ ........................ Source code of the server
│   │── etc/ ........................... Files for demonstration
│   │── dp_project/server/ ............. Proto Auto generated Files
│   │── README.md ...................... This file


```

## **REST API Endpoints**

| Method | Endpoint | Description | Consistency |
|--------|---------|-------------|-------------|
| `GET` | `/kv/keys` | Get all keys | Sequentially Consistent |
| `GET` | `/kv/get?key=KEY` | Get the value of a key | Sequentially Consistent / Linearizable |
| `POST` | `/kv/set?key=KEY&value=VAL` | Set a key-value pair | Linearizable |
| `POST` | `/kv/set-multi` | Set multiple key-value pairs | Linearizable |
| `POST` | `/kv/rmw` | Read-Modify-Write operation | Linearizable |

## **Running the System**

### **1. Build & Start Dockerized Deployment**
```sh
cd src/docker
docker compose up --build -d
```

### **2. Run Tests (From etc/)**
```sh
cd src/etc
chmod +x test.sh
./test.sh
```

### **3. View Logs**
```sh
docker compose logs -f --tail=100
```

## **Failure Tolerance & Recovery**

- **Leader Election**: If a leader fails, etcd detects it and promotes a new leader.
- **Log Replication**: Followers apply committed logs from the leader.
- **Snapshots**: Reduce memory footprint by periodically saving state in etcd.

## **Acknowledgments**
This project follows **Paxos & MultiPaxos concepts** as taught in **Distributed Systems 236351 (Technion)**.
