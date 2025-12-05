package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	discoverygrpc "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	serverv3 "github.com/envoyproxy/go-control-plane/pkg/server/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
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
	var lbEndpoints []*endpoint.LbEndpoint

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
							Route: &route.RouteAction{
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
func addNewCluster(nodeID string, name string, hosts []string, port uint32) error {
	// 1. Get the existing snapshot
	// Ensure 'snap' is explicitly defined if your IDE is confused
	snap, err := snapshotCache.GetSnapshot(nodeID)
	if err != nil {
		// If no snapshot exists, we might need to create a fresh one
		return fmt.Errorf("no snapshot found for node %s: %v", nodeID, err)
	}

	// 2. Prepare the new Cluster
	newCluster := newStaticCluster(name, hosts, port)

	// 3. READ: Use 'types' (Integer) to get existing resources from the array
	//    snap.Resources is an array [8]VersionedResources, indexed by INT.
	existingClusters := snap.GetResources(resource.ClusterType)
	existingRoutes := snap.GetResources(resource.RouteType)
	existingListeners := snap.GetResources(resource.ListenerType)

	// 4. Convert Maps to Slices (Snapshot requires slices)
	//    We append our new cluster to the existing list here.
	var clusters []types.Resource
	for _, c := range existingClusters {
		clusters = append(clusters, c)
	}
	clusters = append(clusters, newCluster)

	var routes []types.Resource
	for _, r := range existingRoutes {
		routes = append(routes, r)
	}

	var listeners []types.Resource
	for _, l := range existingListeners {
		listeners = append(listeners, l)
	}

	// 5. WRITE: Use 'resource' (String) to create the new snapshot
	//    NewSnapshot expects a map keyed by STRING (Type URL).
	newSnap, err := cache.NewSnapshot(
		fmt.Sprintf("%d", time.Now().UnixNano()), // Version must be unique
		map[resource.Type][]types.Resource{
			resource.ClusterType:  clusters,
			resource.RouteType:    routes,
			resource.ListenerType: listeners,
		},
	)
	if err != nil {
		return err
	}

	return snapshotCache.SetSnapshot(context.Background(), nodeID, newSnap)
}

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

func addNewCluster(name string, hosts []string, port uint32) error {
	// 1. Get the existing snapshot
	snap, err := snapshotCache.GetSnapshot(nodeID)
	if err != nil {
		// Handle case where snapshot doesn't exist yet (init new one)
		// For now, we assume it exists or you return error
		return err
	}

	// 2. Prepare the new Cluster
	newCluster := newStaticCluster(name, hosts, port)

	// 3. Extract EXISTING clusters from the map and convert to Slice
	//    Note: snap.Resources[type].Items is a map[string]types.Resource
	var clusters []types.Resource
	for _, cl := range snap.GetResources(resource.ClusterType) {
		clusters = append(clusters, cl)
	}

	// 4. Append the NEW cluster
	clusters = append(clusters, newCluster)

	// 5. You must also preserve Routes and Listeners (convert them to slices too)
	//    or they will be deleted in the new snapshot!
	routes := mapToSlice(snap.GetResources(resource.RouteType))
	listeners := mapToSlice(snap.GetResources(resource.ListenerType))

	// 6. Create the New Snapshot
	//    NewSnapshot expects map[resource.Type][]types.Resource
	newSnap, err := cache.NewSnapshot(
		"2", // Ideally, increment this version number dynamically
		map[resource.Type][]types.Resource{
			resource.ClusterType:  clusters,
			resource.RouteType:    routes,
			resource.ListenerType: listeners,
		},
	)
	if err != nil {
		return err
	}

	return snapshotCache.SetSnapshot(context.Background(), nodeID, newSnap)
}

// Helper to convert the internal Map to a Slice
func mapToSlice(items map[string]types.Resource) []types.Resource {
	var out []types.Resource
	for _, item := range items {
		out = append(out, item)
	}
	return out
}

// ---------------- Main ----------------

func main() {
	// Create snapshot cache
	snapshotCache = cache.NewSnapshotCache(false, cache.IDHash{}, nil)

	// Start xDS server
	srv := serverv3.NewServer(context.Background(), snapshotCache, nil)
	grpcServer := grpc.NewServer()
	discoverygrpc.RegisterAggregatedDiscoveryServiceServer(grpcServer, srv)

	// Set initial snapshot
	if err := setInitialSnapshot(); err != nil {
		log.Fatalf("Failed to set snapshot: %v", err)
	}

	// Optionally, add a new cluster dynamically after startup
	go func() {
		time.Sleep(5 * time.Second)
		if err := addNewCluster(nodeID, "payments_cluster", []string{"127.0.0.2"}, 9090); err != nil {
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
