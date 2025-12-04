package main

import (
	"context"
	"log"
	"net"
	"time"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/anypb"
)

var (
	snapshotCache cache.SnapshotCache
	nodeID        = "node1"
)

// ---------------- Helper Functions ----------------

func duration(seconds int) *time.Duration {
	d := time.Duration(seconds) * time.Second
	return &d
}

func socketAddress(ip string, port uint32) *core.Address {
	return &core.Address{
		Address: &core.Address_SocketAddress{
			SocketAddress: &core.SocketAddress{
				Address: ip,
				PortSpecifier: &core.SocketAddress_PortValue{
					PortValue: port,
				},
			},
		},
	}
}

// Create a static cluster with a list of hosts
func newStaticCluster(name string, hosts []string, port uint32) *cluster.Cluster {
	lbEndpoints := []*endpoint.LbEndpoint{}

	for _, h := range hosts {
		lbEndpoints = append(lbEndpoints, &endpoint.LbEndpoint{
			HostIdentifier: &endpoint.LbEndpoint_Endpoint{
				Endpoint: &endpoint.Endpoint{
					Address: socketAddress(h, port),
				},
			},
		})
	}

	return &cluster.Cluster{
		Name:                 name,
		ConnectTimeout:       duration(1),
		ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_STATIC},
		LbPolicy:             cluster.Cluster_ROUND_ROBIN,
		LoadAssignment: &endpoint.ClusterLoadAssignment{
			ClusterName: name,
			Endpoints: []*endpoint.LocalityLbEndpoints{
				{
					LbEndpoints: lbEndpoints,
				},
			},
		},
	}
}

// Build a simple route configuration
func newRouteConfig(routeName, clusterName string) *route.RouteConfiguration {
	return &route.RouteConfiguration{
		Name: routeName,
		VirtualHosts: []*route.VirtualHost{
			{
				Name:    "backend",
				Domains: []string{"*"},
				Routes: []*route.Route{
					{
						Match: &route.RouteMatch{
							PathSpecifier: &route.RouteMatch_Prefix{Prefix: "/"},
						},
						Action: &route.Route_Route{
							Route: &route.Route_RouteAction{
								Cluster: clusterName,
							},
						},
					},
				},
			},
		},
	}
}

// Create a listener with HTTP Connection Manager
func newListener(name, routeName string, port uint32) *listener.Listener {
	hcm := &listener.HttpConnectionManager{
		StatPrefix: "ingress_http",
		RouteSpecifier: &listener.HttpConnectionManager_RouteConfig{
			RouteConfig: newRouteConfig(routeName, "example_cluster"),
		},
		HttpFilters: []*listener.HttpFilter{
			{Name: wellknown.Router},
		},
	}

	anyHCM, _ := anypb.New(hcm)

	return &listener.Listener{
		Name:    name,
		Address: socketAddress("0.0.0.0", port),
		FilterChains: []*listener.FilterChain{
			{
				Filters: []*listener.Filter{
					{
						Name: wellknown.HTTPConnectionManager,
						ConfigType: &listener.Filter_TypedConfig{
							TypedConfig: anyHCM,
						},
					},
				},
			},
		},
	}
}

// ---------------- Snapshot Management ----------------

// Initialize a snapshot with a cluster, route, listener
func setInitialSnapshot() error {
	clusterRes := newStaticCluster("example_cluster", []string{"127.0.0.1"}, 8080)
	listenerRes := newListener("listener_0", "local_route", 10000)

	snap, err := cache.NewSnapshot(
		"1",
		map[resource.Type][]types.Resource{
			resource.ClusterType:  {clusterRes},
			resource.RouteType:    {newRouteConfig("local_route", "example_cluster")},
			resource.ListenerType: {listenerRes},
		},
	)
	if err != nil {
		return err
	}

	return snapshotCache.SetSnapshot(context.Background(), nodeID, snap)
}

// Add a new cluster dynamically
func addNewCluster(name string, hosts []string, port uint32) error {
	snap, _ := snapshotCache.GetSnapshot(nodeID)

	newCluster := newStaticCluster(name, hosts, port)
	clusters := append(snap.Resources[resource.ClusterType], newCluster)

	newSnap, err := cache.NewSnapshot(
		"2",
		map[resource.Type][]types.Resource{
			resource.ClusterType:  clusters,
			resource.RouteType:    snap.Resources[resource.RouteType],
			resource.ListenerType: snap.Resources[resource.ListenerType],
		},
	)
	if err != nil {
		return err
	}

	return snapshotCache.SetSnapshot(context.Background(), nodeID, newSnap)
}

// ---------------- Main ----------------

func main() {
	// Create snapshot cache
	snapshotCache = cache.NewSnapshotCache(false, cache.IDHash{}, nil)

	// Start xDS server
	srv := serverv3.NewServer(context.Background(), snapshotCache, nil)
	grpcServer := grpc.NewServer()
	serverv3.RegisterServer(grpcServer, srv)

	// Set initial snapshot
	if err := setInitialSnapshot(); err != nil {
		log.Fatalf("Failed to set snapshot: %v", err)
	}

	// Optionally, add a new cluster dynamically after startup
	go func() {
		time.Sleep(5 * time.Second)
		if err := addNewCluster("payments_cluster", []string{"127.0.0.2"}, 9090); err != nil {
			log.Printf("Failed to add new cluster: %v", err)
		} else {
			log.Println("New cluster 'payments_cluster' added")
		}
	}()

	// Listen for Envoy connections
	lis, err := net.Listen("tcp", ":18000")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	log.Println("xDS server running on :18000")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("gRPC server failed: %v", err)
	}
}
