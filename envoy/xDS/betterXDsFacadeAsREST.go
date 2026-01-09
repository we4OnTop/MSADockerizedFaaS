package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	routerv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	clusterservice "github.com/envoyproxy/go-control-plane/envoy/service/cluster/v3"
	discoverygrpc "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	endpointservice "github.com/envoyproxy/go-control-plane/envoy/service/endpoint/v3"
	listenerservice "github.com/envoyproxy/go-control-plane/envoy/service/listener/v3"
	routeservice "github.com/envoyproxy/go-control-plane/envoy/service/route/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	xds "github.com/envoyproxy/go-control-plane/pkg/server/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/anypb"

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	accesslogv3 "github.com/envoyproxy/go-control-plane/envoy/service/accesslog/v3"
)

const (
	GrpcAddressEnv = "0.0.0.0:50051"
	NodeIDEnv      = "local_node"
	RestPortEnv    = "8081"
	RedisAddrEnv   = "redis-messanger:6379"
	rcs            = "redis-messanger:6379"
)

func main() {
	//GrpcAddressEnv := os.Getenv("GRPC_ADDRESS")
	//NodeIDEnv := os.Getenv("NODE_ID")
	//RestPortEnv := os.Getenv("REST_PORT")
	//RestPortEnv := os.Getenv("REST_PORT")
	//RedisAddrENV := os.Getenv("REDIS_ADDR")
	AddFaasRedisTopic := os.Getenv("TRACKING_FAAS_REDIS_TOPIC")
	//RemoveFaasRedisTopic := os.Getenv("REMOVE_FAAS_REDIS_TOPIC")

	log.Printf("RestPortEnv: %v", RestPortEnv)
	log.Printf("RedisAddrENV: %v", RedisAddrEnv)
	log.Printf("AddFaasRedisTopic: %v", AddFaasRedisTopic)

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Initialize Snapshot Cache
	snapshotCache := cache.NewSnapshotCache(false, cache.IDHash{}, nil)

	// Initialize xDS Server
	ctx := context.Background()
	srv := xds.NewServer(ctx, snapshotCache, nil)
	grpcServer := grpc.NewServer()

	// Register Envoy xDS Services
	discoverygrpc.RegisterAggregatedDiscoveryServiceServer(grpcServer, srv)
	endpointservice.RegisterEndpointDiscoveryServiceServer(grpcServer, srv)
	clusterservice.RegisterClusterDiscoveryServiceServer(grpcServer, srv)
	routeservice.RegisterRouteDiscoveryServiceServer(grpcServer, srv)
	listenerservice.RegisterListenerDiscoveryServiceServer(grpcServer, srv)

	// Start gRPC Server
	lis, err := net.Listen("tcp", GrpcAddressEnv)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", GrpcAddressEnv, err)
	}

	// Start Gin REST API for management
	go startGinServer(snapshotCache, ctx, NodeIDEnv, RestPortEnv)

	// Start snapshot inspector in background
	go func() {
		for {
			_, err := snapshotCache.GetSnapshot(NodeIDEnv)
			if err != nil {
				log.Printf("Inspector: Node '%s' has no snapshot yet.", NodeIDEnv)
			}
			time.Sleep(5 * time.Second)
		}
	}()

	rdb := redis.NewClient(&redis.Options{
		Addr: rcs,
	})

	// Test connection redis
	pong, err := rdb.Ping(ctx).Result()
	if err != nil {
		fmt.Println("Could not connect to Redis:", err)
		return
	}
	fmt.Println("Connected to Redis:", pong)
	alsService := &AccessLogService{
		RedisClient: rdb,
	}

	accesslogv3.RegisterAccessLogServiceServer(grpcServer, alsService)
	log.Println("Access Log Service (ALS) registered.")

	log.Printf("Envoy Control Plane running on gRPC %s", GrpcAddressEnv)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("gRPC server exited: %v", err)
	}
}

// Gin REST Server
func startGinServer(snapshotCache cache.SnapshotCache, ctx context.Context, NodeIDEnv string, RestPortEnv string) {
	r := gin.Default()

	//setup functions for cluster and listener routes
	setupClusterRoutes(r, snapshotCache, ctx, NodeIDEnv)
	setupListenerRoutes(r, snapshotCache, ctx)

	r.GET("/snapshot", func(c *gin.Context) {
		snap, err := snapshotCache.GetSnapshot(NodeIDEnv)
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
		if err := snapshotCache.SetSnapshot(context.Background(), NodeIDEnv, snap); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "snapshot updated", "version": version})
	})
	log.Printf("🚀 REST API running on http://localhost:%s", RestPortEnv)
	if err := r.Run(":" + RestPortEnv); err != nil {
		log.Fatalf("Failed to start REST server: %v", err)
	}
}

type AccessLogService struct {
	accesslogv3.UnimplementedAccessLogServiceServer
	RedisClient *redis.Client
}

// Push response information in redis
func (s *AccessLogService) StreamAccessLogs(stream accesslogv3.AccessLogService_StreamAccessLogsServer) error {
	for {
		msg, err := stream.Recv()
		if err != nil {
			return err
		}

		if httpLogs := msg.GetHttpLogs(); httpLogs != nil {
			for _, entry := range httpLogs.LogEntry {

				statusCode := uint32(0)
				if entry.Response != nil && entry.Response.ResponseCode != nil {
					statusCode = entry.Response.ResponseCode.Value
				}

				reqID := "no-id"
				if entry.Request != nil && entry.Request.RequestHeaders != nil {
					if val, ok := entry.Request.RequestHeaders["x-request-id"]; ok {
						reqID = val
					}
				}

				upstreamCluster := "unknown"
				upstreamAddress := "unknown"
				upstreamPort := uint32(0)

				if common := entry.CommonProperties; common != nil {
					upstreamCluster = common.UpstreamCluster

					if remoteAddr := common.UpstreamRemoteAddress; remoteAddr != nil {
						if socketAddr := remoteAddr.GetSocketAddress(); socketAddr != nil {
							upstreamAddress = fmt.Sprintf("%s:%d", socketAddr.Address, socketAddr.GetPortValue())
							upstreamPort = socketAddr.GetPortValue()
						}
					}
				}
				faasName := getEndpointMetadata(upstreamCluster, upstreamPort)

				log.Printf("gRPC Log: Cluster=%s | Endpoint=%s | Status=%d | ReqID=%s",
					upstreamCluster, upstreamAddress, statusCode, reqID)

				if s.RedisClient != nil {
					ctx := context.Background()
					if faasName != "not-found" {

						s.RedisClient.Set(ctx, faasName+":"+faasName, 1, 0) // Added 0 for no expiration on this key
						s.RedisClient.Incr(ctx, faasName+":timer")
						s.RedisClient.Expire(ctx, faasName+":timer", 10*time.Second)
					}
				}
			}
		}
	}
}

type ClusterInput struct {
	Name           string  `json:"name" binding:"required"`
	DnsType        string  `json:"dns-type"`
	HTTPProtocol   string  `json:"http-protocol"`
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

type ClusterHealth struct {
	FaasId string `json:"faas-id" binding:"required"`
	Status string `json:"status" binding:"required"`
}

func setupClusterRoutes(r *gin.Engine, snapshotCache cache.SnapshotCache, ctx context.Context, NodeIDEnv string) {
	r.POST("/cluster", func(c *gin.Context) {
		var input ClusterInput

		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}

		var baseRoutes []Route = input.BaseRoutes
		var fallbackRoutes []Route = input.FallbackRoutes

		if !checkIfClusterNameUnique(input.Name) {
			newCluster := createCluster(input.Name, getDNSType(input.DnsType), getHTTPProtocol(input.HTTPProtocol))
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
		clusters := make([]map[string]interface{}, len(CurrentClusters))
		for i, cl := range CurrentClusters {
			clusters[i] = map[string]interface{}{
				"name": cl.Name,
				"type": cl.GetType().String(),
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
			c.JSON(http.StatusOK, gin.H{"status": "snapshot updated", "version": snapshot.GetVersion(NodeIDEnv)})
			return
		} else {
			c.JSON(http.StatusConflict, "No cluster with name exists")
		}
	})

	r.PUT("/changeHealth", func(c *gin.Context) {
		var input ClusterHealth

		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}

		for _, cl := range CurrentClusters {
			updateHealthIfMetadataMatches(cl.Name, input.FaasId, input.Status)
		}

		snapshot, err := updateSnapshot(snapshotCache, ctx)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "snapshot updated", "version": snapshot})
		return
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
	r.GET("/listener", func(c *gin.Context) {
		listenerMap := make(map[string][]string)
		currentLisConf := getCurrentListenerConfigurations()

		for _, l := range currentLisConf {
			var allClusters []string

			for _, route := range l.Routes {
				allClusters = append(allClusters, route.ClusterToUse)
			}
			listenerMap[l.ListenerConfiguration.ListenerName] = allClusters
		}

		c.JSON(200, gin.H{
			"listeners": listenerMap,
			"count":     len(listenerMap),
		})
	})

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

		var found bool = false

		for i, listener := range CurrentListenerConfigurations {
			if listener.ListenerConfiguration.ListenerName == input.ListenerName {
				routeExists := false
				for _, r := range listener.Routes {
					if r.ClusterToUse == input.Route.ClusterToUse {
						routeExists = true
						break
					} else if r.Prefix == input.Route.Prefix {
						routeExists = true
						break
					}
				}

				if !routeExists {
					CurrentListenerConfigurations[i].Routes = append(listener.Routes, input.Route)
				}

				found = true
				break
			}
		}

		if found {
			var newListeners []*listenerv3.Listener
			for i, c := range CurrentListeners {
				if c.Name != input.ListenerName {
					newListeners = append(newListeners, newListeners[i])
				}
			}
			CurrentListeners = newListeners

		}

		if !found {
			CurrentListeners = []*listenerv3.Listener{}
			index := addClusterToListener(input.Route, input.ListenerName)
			err := createListener(CurrentListenerConfigurations[index].Routes, CurrentListenerConfigurations[index].VirtualHosts, CurrentListenerConfigurations[index].ListenerConfiguration)
			_, err2 := updateSnapshot(snapshotCache, ctx)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			if err2 != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err2.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"status": "snapshot updated", "version": CurrentListeners})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "No change!"})
	})
}

func generateSnapshot(version string) (*cache.Snapshot, error) {
	// Cluster
	cls := &clusterv3.Cluster{
		Name:           "example_backend",
		ConnectTimeout: nil,

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

	// Route Configuration
	// Create the empty Router configuration
	routerConfig, err := anypb.New(&routerv3.Router{})
	if err != nil {
		return nil, err
	}

	// Use it in the HTTP Connection Manager
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

	return snap, nil
}
