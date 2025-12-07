package main

import (
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	// Envoy Control Plane Core
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"

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

var CURRENT_CLUSTERS []*clusterv3.Cluster
var CURRENT_LISTENERS []*listenerv3.Listener

func generateNewSnapshot(version string, cls *clusterv3.Cluster, lis *listenerv3.Listener) cache.Snapshot {
	snap, _ := cache.NewSnapshot(version, map[resource.Type][]types.Resource{
		resource.ClusterType:  {cls},
		resource.ListenerType: {lis},
	})
	return *snap
}

func checkIfClusterNameUnique(clusterName string) bool {
	for _, c := range CURRENT_CLUSTERS {
		if c.Name == clusterName {
			return true
		}
	}
	return false
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
	}

	return &cls
}

func remove(s []uint32, i uint32) []uint32 {
	s[i] = s[len(s)-1]
	return s[:len(s)-1]
}

func appendRoutesToCluster(cls *clusterv3.Cluster, baseRoutes map[string]uint32, fallbackRoutes map[string]uint32) {
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
	addEndpoints := func(priority uint32, routes map[string]uint32) {
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
		for host, port := range routes {
			locality.LbEndpoints = append(locality.LbEndpoints, createHost(host, port))
		}
	}

	// Add base routes
	addEndpoints(baseRoutePriority, baseRoutes)

	// Add fallback routes
	addEndpoints(fallbackPriority, fallbackRoutes)
}

func createClusterWithFallback(name string, clusterName string) *clusterv3.Cluster {
	cls := &clusterv3.Cluster{
		Name: name,
		ClusterDiscoveryType: &clusterv3.Cluster_Type{
			Type: clusterv3.Cluster_STATIC,
		},
		LbPolicy: clusterv3.Cluster_LEAST_REQUEST,
		LoadAssignment: &endpointv3.ClusterLoadAssignment{
			ClusterName: clusterName,
			Endpoints: []*endpointv3.LocalityLbEndpoints{
				// Primary hosts (priority 0)
				{
					Priority: 0,
					LbEndpoints: []*endpointv3.LbEndpoint{
						createHost("10.0.0.0", 9001),
						createHost("10.0.0.0", 9001),
					},
				},
				// Fallback host (priority 1)
				createFallbackRoute(),
			},
		},
		CircuitBreakers: &clusterv3.CircuitBreakers{
			Thresholds: []*clusterv3.CircuitBreakers_Thresholds{{
				Priority:           corev3.RoutingPriority_DEFAULT,
				MaxConnections:     wrapperspb.UInt32(100), // threshold per host
				MaxPendingRequests: wrapperspb.UInt32(50),
			}},
		},
	}

	return cls
}
func createFallbackRoute() *endpointv3.LocalityLbEndpoints {
	fallbackRoute := endpointv3.LocalityLbEndpoints{
		Priority: 1,
		LbEndpoints: []*endpointv3.LbEndpoint{
			createHost("10.0.0.0", 9001),
		},
	}
	return &fallbackRoute
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
