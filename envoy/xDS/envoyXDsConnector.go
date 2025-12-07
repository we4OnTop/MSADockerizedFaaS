package main

import (
	"context"
	"fmt"
	"log"
	"time"

	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	routerv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	// Envoy Control Plane Core
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	// Resource Definitions
	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
)

const (
	baseRoutePriority = 0
	fallbackPriority  = 1
)

var CurrentClusters []*clusterv3.Cluster
var CurrentListeners []*listenerv3.Listener
var CurrentListenerConfigurations []*Config
var snapshotNumber int32 = 1

func checkIfClusterNameUnique(clusterName string) bool {
	for _, c := range CurrentClusters {
		if c.Name == clusterName {
			return true
		}
	}
	return false
}
func toResources[T types.Resource](items []T) []types.Resource {
	out := make([]types.Resource, len(items))
	for i, item := range items {
		out[i] = item
	}
	return out
}

func updateSnapshot(snapshotCache cache.SnapshotCache, ctx context.Context) (*cache.Snapshot, error) {
	log.Printf("Error setting snapshot: %v", CurrentListeners)
	snap, err := cache.NewSnapshot(
		fmt.Sprintf("v%d", snapshotNumber),
		map[resource.Type][]types.Resource{
			resource.ClusterType:  toResources(CurrentClusters),
			resource.ListenerType: toResources(CurrentListeners),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create snapshot: %v", err)
	}

	if err := snapshotCache.SetSnapshot(ctx, NodeID, snap); err != nil {
		return nil, fmt.Errorf("failed to set snapshot: %v", err)
	}
	snapshotNumber = snapshotNumber + 1
	return snap, nil
}

func createCluster(clusterName string) *clusterv3.Cluster {
	cls := clusterv3.Cluster{
		Name:                 clusterName,
		ConnectTimeout:       durationpb.New(time.Second * 1),
		ClusterDiscoveryType: &clusterv3.Cluster_Type{Type: clusterv3.Cluster_STATIC},
		LbPolicy:             clusterv3.Cluster_LEAST_REQUEST,
		LoadAssignment: &endpointv3.ClusterLoadAssignment{
			ClusterName: clusterName,
			Endpoints: []*endpointv3.LocalityLbEndpoints{{
				LbEndpoints: []*endpointv3.LbEndpoint{},
			}},
		},
		CircuitBreakers: &clusterv3.CircuitBreakers{
			Thresholds: []*clusterv3.CircuitBreakers_Thresholds{{
				Priority:           corev3.RoutingPriority_DEFAULT,
				MaxConnections:     wrapperspb.UInt32(100), // threshold per host
				MaxPendingRequests: wrapperspb.UInt32(50),
			}},
		},
	}

	return &cls
}

func appendNewCluster(newCluster *clusterv3.Cluster) {
	CurrentClusters = append(CurrentClusters, newCluster)
}

func removeCluster(clusterName string) {
	var newClusters []*clusterv3.Cluster
	for i, c := range CurrentClusters {
		if c.Name != clusterName {
			newClusters = append(newClusters, newClusters[i])
		}
	}
	CurrentClusters = newClusters
}

type Route struct {
	Address string `json:"address" binding:"required"`
	Port    uint32 `json:"port" binding:"required"`
}

func replaceRoutesOnCluster(cls *clusterv3.Cluster, baseRoutes []Route, fallbackRoutes []Route) {

	cls.LoadAssignment = &endpointv3.ClusterLoadAssignment{}
	cls.LoadAssignment.Endpoints = []*endpointv3.LocalityLbEndpoints{}

	// Track existing priorities
	existingPriorities := map[uint32]*endpointv3.LocalityLbEndpoints{}

	// Helper to create or append to locality
	addEndpoints := func(priority uint32, routes []Route) {
		locality, ok := existingPriorities[priority]
		if !ok {
			// Create new locality
			locality = &endpointv3.LocalityLbEndpoints{
				Priority:    priority,
				LbEndpoints: []*endpointv3.LbEndpoint{},
			}
			cls.LoadAssignment.Endpoints = append(cls.LoadAssignment.Endpoints, locality)
			existingPriorities[priority] = locality
		}
		// Append endpoints
		for _, item := range routes {
			locality.LbEndpoints = append(locality.LbEndpoints, createHost(item.Address, item.Port))
		}
	}

	// Add base routes
	addEndpoints(baseRoutePriority, baseRoutes)

	// Add fallback routes
	addEndpoints(fallbackPriority, fallbackRoutes)
}

func appendRoutesToCluster(cls *clusterv3.Cluster, baseRoutes []Route, fallbackRoutes []Route) {
	// Ensure LoadAssignment exists
	if cls.LoadAssignment == nil {
		cls.LoadAssignment = &endpointv3.ClusterLoadAssignment{}
	}
	if len(cls.LoadAssignment.Endpoints) == 0 {
		cls.LoadAssignment.Endpoints = []*endpointv3.LocalityLbEndpoints{}
	}

	// Track existing priorities
	existingPriorities := map[uint32]*endpointv3.LocalityLbEndpoints{}

	for _, locality := range cls.LoadAssignment.Endpoints {
		existingPriorities[locality.Priority] = locality
	}

	// Helper to create or append to locality
	addEndpoints := func(priority uint32, routes []Route) {
		locality, ok := existingPriorities[priority]
		if !ok {
			// Create new locality
			locality = &endpointv3.LocalityLbEndpoints{
				Priority:    priority,
				LbEndpoints: []*endpointv3.LbEndpoint{},
			}
			cls.LoadAssignment.Endpoints = append(cls.LoadAssignment.Endpoints, locality)
			existingPriorities[priority] = locality
		}
		// Append endpoints
		for _, item := range routes {
			locality.LbEndpoints = append(locality.LbEndpoints, createHost(item.Address, item.Port))
		}
	}

	// Add base routes
	addEndpoints(baseRoutePriority, baseRoutes)

	// Add fallback routes
	addEndpoints(fallbackPriority, fallbackRoutes)

	println(cls)
}

func createHost(addr string, port uint32) *endpointv3.LbEndpoint {
	return &endpointv3.LbEndpoint{
		HostIdentifier: &endpointv3.LbEndpoint_Endpoint{
			Endpoint: &endpointv3.Endpoint{
				Address: &corev3.Address{
					Address: &corev3.Address_SocketAddress{
						SocketAddress: &corev3.SocketAddress{
							Address: addr,
							PortSpecifier: &corev3.SocketAddress_PortValue{
								PortValue: port,
							},
						},
					},
				},
			},
		},
	}
}

type ListenerRoute struct {
	Prefix       string `json:"prefix" binding:"required"`
	ClusterToUse string `json:"cluster_to_use" binding:"required"`
	VirtualHost  string `json:"virtual_host" binding:"required"`
}

type VirtualHost struct {
	Domains []string `json:"domains" binding:"required"`
	Name    string   `json:"name" binding:"required"`
}

type ListenerConfiguration struct {
	ListenerName    string `json:"listener_name" binding:"required"`
	ListenerAddress string `json:"listener_address" binding:"required"`
	ListenerPort    uint32 `json:"listener_port" binding:"required"`
}

type Config struct {
	Routes                []ListenerRoute       `json:"routes" binding:"required"`
	VirtualHosts          []VirtualHost         `json:"virtualHosts" binding:"required"`
	ListenerConfiguration ListenerConfiguration `json:"listenerConfiguration" binding:"required"`
}

func createListener(
	routes []ListenerRoute,
	virtualHosts []VirtualHost,
	listenerConfiguration ListenerConfiguration,
) error {
	// Map routes per virtual host
	vhMap := make(map[string][]*routev3.Route)
	for _, route := range routes {
		vhMap[route.VirtualHost] = append(vhMap[route.VirtualHost], createRouteComponent(route.Prefix, route.ClusterToUse))
	}

	// Build Envoy VirtualHosts
	var envoyVHs []*routev3.VirtualHost
	for _, vh := range virtualHosts {
		mappedRoutes := vhMap[vh.Name] // get routes for this virtual host
		envoyVHs = append(envoyVHs, &routev3.VirtualHost{
			Name:    vh.Name,
			Domains: vh.Domains,
			Routes:  mappedRoutes,
		})
	}

	// --- FIX: Create router filter config ---
	routerConfig, err := anypb.New(&routerv3.Router{})
	if err != nil {
		return fmt.Errorf("failed to create router config: %v", err)
	}

	// HTTP Connection Manager
	hcmConfig := &hcm.HttpConnectionManager{
		StatPrefix: listenerConfiguration.ListenerName,
		RouteSpecifier: &hcm.HttpConnectionManager_RouteConfig{
			RouteConfig: &routev3.RouteConfiguration{
				Name:         listenerConfiguration.ListenerName + "_route",
				VirtualHosts: envoyVHs,
			},
		},
		HttpFilters: []*hcm.HttpFilter{
			{
				Name: wellknown.Router, // Use wellknown constant
				ConfigType: &hcm.HttpFilter_TypedConfig{
					TypedConfig: routerConfig, // Pass empty router config
				},
			},
		},
	}

	anyHCM, err := anypb.New(hcmConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal HCM: %v", err)
	}

	// Listener
	lis := &listenerv3.Listener{
		Name: listenerConfiguration.ListenerName,
		Address: &corev3.Address{
			Address: &corev3.Address_SocketAddress{
				SocketAddress: &corev3.SocketAddress{
					Address:       listenerConfiguration.ListenerAddress,
					PortSpecifier: &corev3.SocketAddress_PortValue{PortValue: listenerConfiguration.ListenerPort},
				},
			},
		},
		FilterChains: []*listenerv3.FilterChain{{
			Filters: []*listenerv3.Filter{{
				Name:       wellknown.HTTPConnectionManager,
				ConfigType: &listenerv3.Filter_TypedConfig{TypedConfig: anyHCM},
			}},
		}},
	}

	CurrentListeners = append(CurrentListeners, lis)
	return nil
}

func addClusterToListener(route ListenerRoute, listenerName string) int {
	for i, config := range CurrentListenerConfigurations {
		if config.ListenerConfiguration.ListenerName == listenerName {
			changedConfig := config
			changedConfig.Routes = append(config.Routes, route)
			CurrentListenerConfigurations[i] = config
			return i
		}
	}
	return -1
}

func removeClusterFromListener(route ListenerRoute, listenerName string) bool {
	for i, config := range CurrentListenerConfigurations {
		if config.ListenerConfiguration.ListenerName == listenerName {
			changedConfig := config
			for _, listenerRoute := range config.Routes {
				if listenerRoute != route {
					changedConfig.Routes = append(changedConfig.Routes, listenerRoute)
				}
			}
			CurrentListenerConfigurations[i] = changedConfig
			return true
		}
	}
	return false
}

func removeClusterFromListenerGlobal(clusterToRemove string, listenerName string) bool {
	for i, config := range CurrentListenerConfigurations {
		if config.ListenerConfiguration.ListenerName == listenerName {
			changedConfig := config
			for _, listenerRoute := range config.Routes {
				if listenerRoute.ClusterToUse != clusterToRemove {
					changedConfig.Routes = append(changedConfig.Routes, listenerRoute)
				}
			}
			CurrentListenerConfigurations[i] = changedConfig
			return true
		}
	}
	return false
}

func createRouteComponent(prefix string, routeToCluster string) *routev3.Route {
	route := &routev3.Route{
		Match: &routev3.RouteMatch{
			PathSpecifier: &routev3.RouteMatch_Prefix{Prefix: prefix},
		},
		Action: &routev3.Route_Route{
			Route: &routev3.RouteAction{
				ClusterSpecifier: &routev3.RouteAction_Cluster{
					Cluster: routeToCluster,
				},
			},
		},
	}
	return route
}
