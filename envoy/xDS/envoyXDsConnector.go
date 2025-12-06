package main

import (
	"context"
	"fmt"
	"log"
	"net"
	// Generic xDS imports
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/anypb"

	// Envoy Control Plane Core
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	xds "github.com/envoyproxy/go-control-plane/pkg/server/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"

	// Discovery Services (The xDS APIs)
	clusterservice "github.com/envoyproxy/go-control-plane/envoy/service/cluster/v3"
	discoverygrpc "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	endpointservice "github.com/envoyproxy/go-control-plane/envoy/service/endpoint/v3"
	listenerservice "github.com/envoyproxy/go-control-plane/envoy/service/listener/v3"
	routeservice "github.com/envoyproxy/go-control-plane/envoy/service/route/v3"

	// Resource Definitions
	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
)

// CONFIGURATION BASED ON YOUR YAML
const (
	GrpcPort = ":50051"     // Matches 'port_value: 50051' in your YAML
	NodeID   = "local_node" // Matches 'id: local_node' in your YAML
)

func main() {
	// 1. Initialize the Snapshot Cache
	// We use IDHash{} so it matches based on the Node ID string exactly
	snapshotCache := cache.NewSnapshotCache(false, cache.IDHash{}, nil)

	// 2. Initialize the xDS Server
	ctx := context.Background()
	srv := xds.NewServer(ctx, snapshotCache, nil)
	grpcServer := grpc.NewServer()

	// 3. Register xDS implementations
	discoverygrpc.RegisterAggregatedDiscoveryServiceServer(grpcServer, srv)
	endpointservice.RegisterEndpointDiscoveryServiceServer(grpcServer, srv)
	clusterservice.RegisterClusterDiscoveryServiceServer(grpcServer, srv)
	routeservice.RegisterRouteDiscoveryServiceServer(grpcServer, srv)
	listenerservice.RegisterListenerDiscoveryServiceServer(grpcServer, srv)

	// 4. Create the Listener (The socket)
	lis, err := net.Listen("tcp", GrpcPort)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", GrpcPort, err)
	}

	// 5. SEED DATA: Create an initial snapshot
	// Without this, Envoy connects but receives nothing.
	log.Println("Creating initial snapshot...")
	snap := generateSnapshot("v1")
	if err := snapshotCache.SetSnapshot(ctx, NodeID, &snap); err != nil {
		log.Printf("Snapshot error: %v", err)
	}

	// 6. Start the "Inspector" Loop (IN A GOROUTINE)
	// IMPORTANT: This must be in a 'go func', otherwise it blocks the server start!
	go func() {
		for {
			PrintXdsSnapshot(snapshotCache, NodeID)
		}
	}()

	log.Printf("Control Plane running on %s. Waiting for Envoy node: %s", GrpcPort, NodeID)

	// 7. Start Serving
	if err := grpcServer.Serve(lis); err != nil {
		fmt.Printf("Server error: %v", err)
	}
}

// --- YOUR INSPECTOR FUNCTION (Slightly polished) ---

func PrintXdsSnapshot(c cache.SnapshotCache, nodeID string) {
	snap, err := c.GetSnapshot(nodeID)
	if err != nil {
		// Only log this if you want to see noise before Envoy connects
		// log.Printf("xDS: No snapshot found for node '%s'", nodeID)
		return
	}

	// Fetch Cluster Resources
	clusterResources := snap.GetResources(resource.ClusterType)

	fmt.Printf("\n--- xDS Snapshot Version: %s (Node: %s) ---\n", snap.GetVersion(resource.ClusterType), nodeID)
	fmt.Printf("%-20s | %-15s | %s\n", "CLUSTER NAME", "TYPE", "LB POLICY")
	fmt.Println("-------------------------------------------------------------")

	for _, res := range clusterResources {
		cluster, ok := res.(*clusterv3.Cluster)
		if !ok {
			continue
		}

		cw := "Static/DNS"
		if cluster.GetType() == clusterv3.Cluster_EDS {
			cw = "EDS (Dynamic)"
		}

		fmt.Printf("%-20s | %-15s | %s\n",
			cluster.Name,
			cw,
			cluster.LbPolicy.String(),
		)
	}
	fmt.Println("-------------------------------------------------------------")
}

// --- HELPER: Generate a Sample Snapshot ---

func generateSnapshot(version string) cache.Snapshot {
	// 1. Create a Cluster (Upstream)
	cls := &clusterv3.Cluster{
		Name:                 "example_backend",
		ConnectTimeout:       nil, // Use default
		ClusterDiscoveryType: &clusterv3.Cluster_Type{Type: clusterv3.Cluster_STATIC},
		LbPolicy:             clusterv3.Cluster_ROUND_ROBIN,
		LoadAssignment: &endpointv3.ClusterLoadAssignment{
			ClusterName: "example_backend",
			Endpoints: []*endpointv3.LocalityLbEndpoints{{
				LbEndpoints: []*endpointv3.LbEndpoint{{
					HostIdentifier: &endpointv3.LbEndpoint_Endpoint{
						Endpoint: &endpointv3.Endpoint{
							Address: &corev3.Address{
								Address: &corev3.Address_SocketAddress{
									SocketAddress: &corev3.SocketAddress{
										Address: "8.8.8.8", // Example: Google DNS
										PortSpecifier: &corev3.SocketAddress_PortValue{
											PortValue: 53,
										},
									},
								},
							},
						},
					},
				}},
			}},
		},
	}

	// 2. Create a Listener (Downstream - what Envoy listens on)
	// This will make Envoy open port 10000 on the container
	hcmConfig := &hcm.HttpConnectionManager{
		StatPrefix: "ingress_http",
		RouteSpecifier: &hcm.HttpConnectionManager_RouteConfig{
			RouteConfig: &routev3.RouteConfiguration{
				Name: "local_route",
				VirtualHosts: []*routev3.VirtualHost{{
					Name:    "local_service",
					Domains: []string{"*"},
					Routes: []*routev3.Route{{
						Match: &routev3.RouteMatch{
							PathSpecifier: &routev3.RouteMatch_Prefix{Prefix: "/"},
						},
						Action: &routev3.Route_Route{
							Route: &routev3.RouteAction{
								ClusterSpecifier: &routev3.RouteAction_Cluster{
									Cluster: "example_backend",
								},
							},
						},
					}},
				}},
			},
		},
		HttpFilters: []*hcm.HttpFilter{
			{Name: wellknown.Router},
		},
	}
	anyHCM, _ := anypb.New(hcmConfig)

	lis := &listenerv3.Listener{
		Name: "listener_0",
		Address: &corev3.Address{
			Address: &corev3.Address_SocketAddress{
				SocketAddress: &corev3.SocketAddress{
					Address:       "0.0.0.0",
					PortSpecifier: &corev3.SocketAddress_PortValue{PortValue: 10000},
				},
			},
		},
		FilterChains: []*listenerv3.FilterChain{{
			Filters: []*listenerv3.Filter{{
				Name: wellknown.HTTPConnectionManager,
				ConfigType: &listenerv3.Filter_TypedConfig{
					TypedConfig: anyHCM,
				},
			}},
		}},
	}

	// 3. Construct the Snapshot
	snap, _ := cache.NewSnapshot(version, map[resource.Type][]types.Resource{
		resource.ClusterType:  {cls},
		resource.ListenerType: {lis},
	})
	return *snap
}
