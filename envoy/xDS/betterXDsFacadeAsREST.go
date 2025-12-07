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

	// 6. Start Gin REST API for management
	go startGinServer(snapshotCache, ctx)

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
func startGinServer(snapshotCache cache.SnapshotCache, ctx context.Context) {
	r := gin.Default()

	setupClusterRoutes(r, snapshotCache, ctx)
	setupListenerRoutes(r, snapshotCache, ctx)

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

	log.Printf("🚀 REST API running on http://localhost:%s", RestPort)
	if err := r.Run(":" + RestPort); err != nil {
		log.Fatalf("Failed to start REST server: %v", err)
	}
}

type ClusterInput struct {
	Name           string  `json:"name" binding:"required"`
	BaseRoutes     []Route `json:"base-routes"`
	FallbackRoutes []Route `json:"fallback-routes"`
}
type ClusterRemove struct {
	Name string `json:"name" binding:"required"`
}

type ClusterReplace struct {
	ClusterInput
	Replace bool `json:"replace"`
}

func setupClusterRoutes(r *gin.Engine, snapshotCache cache.SnapshotCache, ctx context.Context) {
	r.POST("/cluster", func(c *gin.Context) {
		var input ClusterInput

		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}

		var baseRoutes []Route = input.BaseRoutes
		var fallbackRoutes []Route = input.FallbackRoutes

		if !checkIfClusterNameUnique(input.Name) {
			//BUILD ALL ON SAVED LOCAL -> EASY
			newCluster := createCluster(input.Name)
			if len(baseRoutes) > 0 || len(fallbackRoutes) > 0 {
				appendRoutesToCluster(newCluster, baseRoutes, fallbackRoutes)
			}
			appendNewCluster(newCluster)
			snapshot, err := updateSnapshot(snapshotCache, ctx)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"status": "snapshot updated", "version": snapshot})
			return
		} else {
			c.JSON(http.StatusConflict, "Name already exists")
		}

	})
	r.GET("/cluster", func(c *gin.Context) {
		// Convert clusters to a JSON-friendly format if needed
		clusters := make([]map[string]interface{}, len(CurrentClusters))
		for i, cl := range CurrentClusters {
			clusters[i] = map[string]interface{}{
				"name": cl.Name,
				"type": cl.GetType().String(),
				// Add more fields if needed
			}
		}

		c.JSON(200, gin.H{
			"clusters": clusters,
			"count":    len(clusters),
		})
	})
	r.DELETE("/cluster", func(c *gin.Context) {
		var input ClusterRemove
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		if checkIfClusterNameUnique(input.Name) {
			removeCluster(input.Name)
			snapshot, err := updateSnapshot(snapshotCache, ctx)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"status": "snapshot updated", "version": snapshot.GetVersion(NodeID)})
			return
		} else {
			c.JSON(http.StatusConflict, "No cluster with name exists")
		}
	})
	r.PUT("/cluster", func(c *gin.Context) {
		var input ClusterReplace

		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}

		var baseRoutes []Route = input.BaseRoutes
		var fallbackRoutes []Route = input.FallbackRoutes
		var foundCluster *clusterv3.Cluster
		for _, cluster := range CurrentClusters {
			if cluster.Name == input.Name {
				foundCluster = cluster
				break
			}
		}

		if foundCluster != nil {
			removeCluster(foundCluster.Name)
			if input.Replace && (len(baseRoutes) > 0 || len(fallbackRoutes) > 0) {
				replaceRoutesOnCluster(foundCluster, baseRoutes, fallbackRoutes)
			} else {
				appendRoutesToCluster(foundCluster, baseRoutes, fallbackRoutes)
			}

			appendNewCluster(foundCluster)
			snapshot, err := updateSnapshot(snapshotCache, ctx)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"status": "snapshot updated", "version": snapshot})
			return
		} else {
			c.JSON(http.StatusConflict, "Cluster not found")
		}
	})
}

type ListenerAddClusterInput struct {
	Route        ListenerRoute `json:"route" binding:"required"`
	ListenerName string        `json:"listener_name" binding:"required"`
}

func setupListenerRoutes(r *gin.Engine, snapshotCache cache.SnapshotCache, ctx context.Context) {
	r.POST("/listener", func(c *gin.Context) {
		var input Config

		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		CurrentListenerConfigurations = append(CurrentListenerConfigurations, &input)

		CurrentListeners = []*listenerv3.Listener{}
		for _, listenerConfiguration := range CurrentListenerConfigurations {
			err := createListener(listenerConfiguration.Routes, listenerConfiguration.VirtualHosts, listenerConfiguration.ListenerConfiguration)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}
		_, err := updateSnapshot(snapshotCache, ctx)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "snapshot updated", "version": CurrentListeners})
	})
	r.PUT("/listener", func(c *gin.Context) {
		var input ListenerAddClusterInput

		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		CurrentListeners = []*listenerv3.Listener{}
		index := addClusterToListener(input.Route, input.ListenerName)
		err := createListener(CurrentListenerConfigurations[index].Routes, CurrentListenerConfigurations[index].VirtualHosts, CurrentListenerConfigurations[index].ListenerConfiguration)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "snapshot updated", "version": CurrentListeners})
	})
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
