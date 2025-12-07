package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	routerv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	xds "github.com/envoyproxy/go-control-plane/pkg/server/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"

	clusterservice "github.com/envoyproxy/go-control-plane/envoy/service/cluster/v3"
	discoverygrpc "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	endpointservice "github.com/envoyproxy/go-control-plane/envoy/service/endpoint/v3"
	listenerservice "github.com/envoyproxy/go-control-plane/envoy/service/listener/v3"
	routeservice "github.com/envoyproxy/go-control-plane/envoy/service/route/v3"

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
)

const (
	GrpcPort = "0.0.0.0:50051"
	NodeID   = "local_node"
	RestPort = "8081"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// 1. Initialize Snapshot Cache
	snapshotCache := cache.NewSnapshotCache(false, cache.IDHash{}, nil)

	// 2. Initialize xDS Server
	ctx := context.Background()
	srv := xds.NewServer(ctx, snapshotCache, nil)
	grpcServer := grpc.NewServer()

	// 3. Register Envoy xDS Services
	discoverygrpc.RegisterAggregatedDiscoveryServiceServer(grpcServer, srv)
	endpointservice.RegisterEndpointDiscoveryServiceServer(grpcServer, srv)
	clusterservice.RegisterClusterDiscoveryServiceServer(grpcServer, srv)
	routeservice.RegisterRouteDiscoveryServiceServer(grpcServer, srv)
	listenerservice.RegisterListenerDiscoveryServiceServer(grpcServer, srv)

	// 4. Start gRPC Server
	lis, err := net.Listen("tcp", GrpcPort)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", GrpcPort, err)
	}

	// 5. Create initial snapshot
	snap, err := generateSnapshot("v1")
	if err != nil {
		log.Fatalf("Error generating snapshot: %v", err)
	}
	if err := snapshotCache.SetSnapshot(ctx, NodeID, snap); err != nil {
		log.Fatalf("Error setting snapshot: %v", err)
	}

	// 6. Start Gin REST API for management
	go startGinServer(snapshotCache)

	// 7. Start snapshot inspector in background
	go func() {
		for {
			_, err := snapshotCache.GetSnapshot(NodeID)
			if err != nil {
				log.Printf("Inspector: Node '%s' has no snapshot yet.", NodeID)
			}
			time.Sleep(5 * time.Second)
		}
	}()

	log.Printf("🚀 Envoy Control Plane running on gRPC %s", GrpcPort)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("gRPC server exited: %v", err)
	}
}

// Gin REST Server
func startGinServer(snapshotCache cache.SnapshotCache) {
	r := gin.Default()

	r.GET("/snapshot", func(c *gin.Context) {
		snap, err := snapshotCache.GetSnapshot(NodeID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Snapshot not found"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"clusters":  snap.GetResources(resource.ClusterType),
			"listeners": snap.GetResources(resource.ListenerType),
		})
	})

	r.POST("/snapshot/:version", func(c *gin.Context) {
		version := c.Param("version")
		snap, err := generateSnapshot(version)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := snapshotCache.SetSnapshot(context.Background(), NodeID, snap); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "snapshot updated", "version": version})
	})

	r.POST("/cluster", func(c *gin.Context) {
		cluster := createCluster("")
		CURRENT_CLUSTERS = append(CURRENT_CLUSTERS, cluster)
		if checkIfClusterNameUnique("Name") {
			//BUILD ALL ON SAVED LOCAL -> EASY
		}
	})

	log.Printf("🚀 REST API running on http://localhost:%s", RestPort)
	if err := r.Run(":" + RestPort); err != nil {
		log.Fatalf("Failed to start REST server: %v", err)
	}
}

// Fixed generateSnapshot (Returns error now)
func generateSnapshot(version string) (*cache.Snapshot, error) {
	// Cluster
	cls := &clusterv3.Cluster{
		Name:           "example_backend",
		ConnectTimeout: nil,

		// CHANGE THIS: Use LOGICAL_DNS so Envoy can resolve the hostname "control-plane"
		ClusterDiscoveryType: &clusterv3.Cluster_Type{Type: clusterv3.Cluster_LOGICAL_DNS},

		LbPolicy: clusterv3.Cluster_ROUND_ROBIN,
		LoadAssignment: &endpointv3.ClusterLoadAssignment{
			ClusterName: "example_backend",
			Endpoints: []*endpointv3.LocalityLbEndpoints{{
				LbEndpoints: []*endpointv3.LbEndpoint{{
					HostIdentifier: &endpointv3.LbEndpoint_Endpoint{
						Endpoint: &endpointv3.Endpoint{
							Address: &corev3.Address{
								Address: &corev3.Address_SocketAddress{
									SocketAddress: &corev3.SocketAddress{

										// CHANGE THIS: Point to your host machine
										Address: "8.8.8.8",

										// CHANGE THIS: The port your Python server is on
										PortSpecifier: &corev3.SocketAddress_PortValue{
											PortValue: 8080,
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

	// ... inside the VirtualHost section ...

	// Route Configuration
	// 1. Create the empty Router configuration
	routerConfig, err := anypb.New(&routerv3.Router{})
	if err != nil {
		return nil, err
	}

	// 2. Use it in the HTTP Connection Manager
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
								// REMOVE the HostRewriteSpecifier (Delete the lines below)
								// HostRewriteSpecifier: &routev3.RouteAction_HostRewriteLiteral{
								//    HostRewriteLiteral: "example.com",
								// },
							},
						},
					}},
				}},
			},
		},
		// --- THE FIX IS HERE ---
		HttpFilters: []*hcm.HttpFilter{
			{
				Name: wellknown.Router,
				ConfigType: &hcm.HttpFilter_TypedConfig{
					TypedConfig: routerConfig, // <--- Passes the empty config
				},
			},
		},
	}

	anyHCM, err := anypb.New(hcmConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal HCM: %v", err)
	}

	// Listener
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

	// Create Snapshot
	snap, err := cache.NewSnapshot(version, map[resource.Type][]types.Resource{
		resource.ClusterType:  {cls},
		resource.ListenerType: {lis},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create snapshot: %v", err)
	}

	// We return a pointer to the snapshot
	return snap, nil
}
