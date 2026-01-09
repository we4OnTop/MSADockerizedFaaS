package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	accesslogv3 "github.com/envoyproxy/go-control-plane/envoy/config/accesslog/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	tracev3 "github.com/envoyproxy/go-control-plane/envoy/config/trace/v3"
	accessloggrpc "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/grpc/v3"
	routerv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	httpv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"
	matcherv3 "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
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
var ListenerUsingClusters = make(map[string][]string)
var ClusterBlockList = []string{}
var snapshotNumber int32 = 1

func checkIfClusterNameUnique(clusterName string) bool {
	for _, c := range CurrentClusters {
		if c.Name == clusterName {
			return true
		}
	}
	return false
}

func getEndpointMetadata(clusterName string, searchPort uint32) string {
	for _, cluster := range CurrentClusters {
		print("Searching cluster... ", clusterName)
		print("Found cluster... ", cluster.Name)
		if cluster.Name == clusterName {
			if cluster.LoadAssignment == nil {
				continue
			}
			for _, locality := range cluster.LoadAssignment.Endpoints {
				for _, lbEndpoint := range locality.LbEndpoints {
					print("Address found: ", lbEndpoint.GetEndpoint().GetAddress())
					print("Metadata found: ", lbEndpoint.GetMetadata().GetFilterMetadata())
					port := lbEndpoint.GetEndpoint().GetAddress().GetSocketAddress().GetPortValue()
					if port == searchPort {
						meta := lbEndpoint.GetMetadata().GetFilterMetadata()["envoy.lb"]
						if meta != nil {
							if val, ok := meta.Fields["faas_id"]; ok {
								return val.GetStringValue()
							}
						}
						return "found-but-no-metadata"
					}
				}
			}
		}
	}
	return "not-found"
}

var registeredDNSTypes = map[string]clusterv3.Cluster_DiscoveryType{
	"strict": clusterv3.Cluster_STRICT_DNS,
	"static": clusterv3.Cluster_STATIC,
}

func getDNSType(typeName string) clusterv3.Cluster_DiscoveryType {
	if value, ok := registeredDNSTypes[typeName]; ok {
		return value
	}
	return clusterv3.Cluster_STRICT_DNS
}

var registeredHTTPProtocol = map[string]*httpv3.HttpProtocolOptions_ExplicitHttpConfig{
	"http1": basicHttp1Config(),
	"http2": basicHttp2Config(),
}

func getHTTPProtocol(protocolName string) *httpv3.HttpProtocolOptions_ExplicitHttpConfig {
	if value, ok := registeredHTTPProtocol[protocolName]; ok {
		return value
	}
	return basicHttp2Config()
}

func toResources[T types.Resource](items []T) []types.Resource {
	out := make([]types.Resource, len(items))
	for i, item := range items {
		out[i] = item
	}
	return out
}

func updateSnapshot(snapshotCache cache.SnapshotCache, ctx context.Context) (*cache.Snapshot, error) {
	// 1. Snapshot-Objekt erstellen
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

	NodeIDEnv := os.Getenv("NODE_ID")

	// 2. Snapshot im Cache setzen (Pusht die Config an Envoy)
	if err := snapshotCache.SetSnapshot(ctx, NodeIDEnv, snap); err != nil {
		return nil, fmt.Errorf("failed to set snapshot: %v", err)
	}

	// --- Ab hier war der Push erfolgreich ---

	// 3. Zähler erst nach Erfolg erhöhen
	snapshotNumber++

	// 4. Blockliste an Nginx/Gatekeeper pushen
	// Da der Snapshot bereits gesetzt ist, ist dieser Schritt kritisch für die Synchronisation
	if err := pushClusterBlockList(); err != nil {
		// Wir loggen den Fehler, geben aber den Snapshot zurück,
		// da Envoy die neuen Cluster bereits kennt.
		log.Printf("CRITICAL: Snapshot set but failed to push blocklist: %v", err)
		return snap, fmt.Errorf("snapshot active but blocklist sync failed: %v", err)
	}

	log.Printf("Successfully updated snapshot v%d and pushed blocklist", snapshotNumber-1)
	return snap, nil
}

func createCluster(clusterName string, discoveryType clusterv3.Cluster_DiscoveryType, coreHttpConfig *httpv3.HttpProtocolOptions_ExplicitHttpConfig) *clusterv3.Cluster {
	httpProtocolOptions := &httpv3.HttpProtocolOptions{
		UpstreamProtocolOptions: &httpv3.HttpProtocolOptions_ExplicitHttpConfig_{
			ExplicitHttpConfig: coreHttpConfig,
		},
	}

	//Converting the configuration to an anypb.Any Type
	pbst, err := anypb.New(httpProtocolOptions)
	if err != nil {
		// Panic error
		panic(err)
	}

	//Define an Cluster
	cls := clusterv3.Cluster{
		Name:                 clusterName,
		ConnectTimeout:       durationpb.New(time.Second * 2),
		ClusterDiscoveryType: &clusterv3.Cluster_Type{Type: discoveryType},
		LbPolicy:             clusterv3.Cluster_ROUND_ROBIN,
		LoadAssignment: &endpointv3.ClusterLoadAssignment{
			ClusterName: clusterName,
			Endpoints: []*endpointv3.LocalityLbEndpoints{{
				LbEndpoints: []*endpointv3.LbEndpoint{},
			}},
		},
		CommonLbConfig: &clusterv3.Cluster_CommonLbConfig{
			HealthyPanicThreshold: &typev3.Percent{
				Value: 0.0,
			},
		},
		CircuitBreakers: &clusterv3.CircuitBreakers{
			// 1. GLOBALE EINSTELLUNGEN ("Der Türsteher & Der Warteraum")
			// Damit die Queue anspringt, muss Envoy hier denken: "Der Laden ist voll."
			Thresholds: []*clusterv3.CircuitBreakers_Thresholds{{
				Priority: corev3.RoutingPriority_DEFAULT,

				// HIER MUSST DU RECHNEN: Anzahl Endpoints * 2
				// Beispiel: Bei 10 Endpoints setzt du das hier auf 20.
				// Sobald der 21. Request kommt, greift die Queue unten.
				MaxConnections: wrapperspb.UInt32(2),
				MaxRequests:    wrapperspb.UInt32(2), // Wichtig für HTTP/2 oder gRPC
				// DIE QUEUE ("Der Warteraum")
				// Hier warten alle Requests, die nicht durch das Limit oben passen.
				MaxPendingRequests: wrapperspb.UInt32(10000),
			}},

			// 2. PRO ENDPUNKT EINSTELLUNGEN ("Der Sitzplatz")
			// Das stellt sicher, dass auch wirklich kein einzelner Server mehr als 2 bekommt.
			PerHostThresholds: []*clusterv3.CircuitBreakers_Thresholds{{
				Priority: corev3.RoutingPriority_DEFAULT,

				// Das harte Limit pro IP
				MaxConnections: wrapperspb.UInt32(2),
			}},
		},
		TypedExtensionProtocolOptions: map[string]*anypb.Any{
			"envoy.extensions.upstreams.http.v3.HttpProtocolOptions": pbst,
		},
	}

	return &cls
}

func basicHttp2Config() *httpv3.HttpProtocolOptions_ExplicitHttpConfig {
	return &httpv3.HttpProtocolOptions_ExplicitHttpConfig{
		ProtocolConfig: &httpv3.HttpProtocolOptions_ExplicitHttpConfig_Http2ProtocolOptions{
			Http2ProtocolOptions: &corev3.Http2ProtocolOptions{},
		},
	}
}

func basicHttp1Config() *httpv3.HttpProtocolOptions_ExplicitHttpConfig {
	return &httpv3.HttpProtocolOptions_ExplicitHttpConfig{
		ProtocolConfig: &httpv3.HttpProtocolOptions_ExplicitHttpConfig_HttpProtocolOptions{
			HttpProtocolOptions: &corev3.Http1ProtocolOptions{},
		},
	}
}

func appendNewCluster(newCluster *clusterv3.Cluster) {
	CurrentClusters = append(CurrentClusters, newCluster)
}

func removeCluster(clusterName string) {
	var newClusters []*clusterv3.Cluster
	print("HAHAHHAHHAHDSHH")
	print(CurrentClusters)
	for _, c := range CurrentClusters {
		print(c.Name)
		print(c)
		if c.Name != clusterName {
			print("HAHAHHAHHAHDSHH22222222")
			newClusters = append(newClusters, c)
		}
	}
	CurrentClusters = newClusters

}

type Route struct {
	Address string `json:"address" binding:"required"`
	Port    uint32 `json:"port" binding:"required"`
	Status  string `json:"status"`
}

func replaceRoutesOnCluster(cls *clusterv3.Cluster, baseRoutes []Route, fallbackRoutes []Route) {

	// 1. Sicherstellen, dass das Struct existiert und den Namen hat
	if cls.LoadAssignment == nil {
		cls.LoadAssignment = &endpointv3.ClusterLoadAssignment{
			ClusterName: cls.Name,
		}
	} else {
		// Falls es existiert, sicherstellen, dass der Name stimmt (optional, aber gut)
		if cls.LoadAssignment.ClusterName == "" {
			cls.LoadAssignment.ClusterName = cls.Name
		}
	}

	// 2. WICHTIG: Statt das Objekt zu ersetzen, setzen wir nur die Endpoints auf leer zurück.
	// Damit behalten wir den Pointer und den ClusterName.
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
			locality.LbEndpoints = append(locality.LbEndpoints, createHost(item.Address, item.Port, item.Status))
		}
	}

	// Add routes
	addEndpoints(baseRoutePriority, baseRoutes)
	addEndpoints(fallbackPriority, fallbackRoutes)

	routesThere := hasHealthyEndpoints(baseRoutes)
	err := updateBlockList(cls.Name, routesThere)
	if err != nil {
		return
	}
}

func hasHealthyEndpoints(routes []Route) bool {
	for _, route := range routes {
		if strings.ToLower(route.Status) != "draining" && strings.ToLower(route.Status) == "unhealthy" {
			return true
		}
	}
	return len(routes) > 0
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
			locality.LbEndpoints = append(locality.LbEndpoints, createHost(item.Address, item.Port, item.Status))
		}

	}

	// Add routes
	addEndpoints(baseRoutePriority, baseRoutes)
	addEndpoints(fallbackPriority, fallbackRoutes)

	routesThere := hasHealthyEndpoints(baseRoutes)
	err := updateBlockList(cls.Name, routesThere)
	if err != nil {
		return
	}
	println(cls)
}

func createHost(addr string, port uint32, status string) *endpointv3.LbEndpoint {
	baseStatus := corev3.HealthStatus(corev3.HealthStatus_HEALTHY)
	if strings.ToLower(status) == "draining" {
		baseStatus = corev3.HealthStatus_DRAINING
	} else if strings.ToLower(status) == "unhealthy" {
		baseStatus = corev3.HealthStatus_UNHEALTHY
	}

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
		Metadata: &corev3.Metadata{
			FilterMetadata: map[string]*structpb.Struct{
				"envoy.lb": {
					Fields: map[string]*structpb.Value{
						"faas_id": structpb.NewStringValue(addr),
					},
				},
			},
		},
		HealthStatus: baseStatus,
	}
}

func updateHealthIfMetadataMatches(
	clusterName string,
	targetFaasID string,
	status string,
) bool {
	baseStatus := corev3.HealthStatus(corev3.HealthStatus_HEALTHY)
	if strings.ToLower(status) == "draining" {
		baseStatus = corev3.HealthStatus_DRAINING
	} else if strings.ToLower(status) == "unhealthy" {
		baseStatus = corev3.HealthStatus_UNHEALTHY
	}
	for _, cluster := range CurrentClusters {
		if cluster.Name != clusterName || cluster.LoadAssignment == nil {
			continue
		}

		for _, locality := range cluster.LoadAssignment.Endpoints {
			for _, lbEndpoint := range locality.LbEndpoints {

				metadata := lbEndpoint.GetMetadata()
				lbMeta, ok := metadata.GetFilterMetadata()["envoy.lb"]
				if !ok {
					continue
				}

				faasIDField, ok := lbMeta.Fields["faas_id"]
				if !ok || faasIDField.GetStringValue() != targetFaasID {
					continue
				}

				lbEndpoint.HealthStatus = baseStatus
				log.Printf("✅ Health Updated: Cluster=%s, FaasID=%s -> %s",
					clusterName, targetFaasID, baseStatus.String())
				return true
			}
		}
	}

	return false
}

type RegexRewriteDefinition struct {
	Regex        string `json:"regex" binding:"required"`
	Substitution string `json:"substitution" binding:"required"`
}
type ListenerRoute struct {
	Prefix       string                  `json:"prefix" binding:"required"`
	ClusterToUse string                  `json:"cluster_to_use" binding:"required"`
	RegexRewrite *RegexRewriteDefinition `json:"regex_rewrite"`
	VirtualHost  string                  `json:"virtual_host" binding:"required"`
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
	//Map routes per virtual host
	vhMap := make(map[string][]*routev3.Route)
	for _, route := range routes {
		regexRewrite := route.RegexRewrite
		vhMap[route.VirtualHost] = append(vhMap[route.VirtualHost], createRouteComponent(route.Prefix, route.ClusterToUse, regexRewrite))
	}

	//Build Envoy VirtualHosts
	var envoyVHs []*routev3.VirtualHost
	for _, vh := range virtualHosts {
		mappedRoutes := vhMap[vh.Name] // get routes for this virtual host
		envoyVHs = append(envoyVHs, &routev3.VirtualHost{
			Name:    vh.Name,
			Domains: vh.Domains,
			Routes:  mappedRoutes,
		})
	}

	//Create router filter config
	routerConfig, err := anypb.New(&routerv3.Router{})
	if err != nil {
		return fmt.Errorf("failed to create router config: %v", err)
	}

	zipkinConfig := &tracev3.ZipkinConfig{
		CollectorCluster:         "jaeger",        // Must match your cluster name
		CollectorEndpoint:        "/api/v2/spans", // Standard Jaeger endpoint
		TraceId_128Bit:           true,
		CollectorEndpointVersion: tracev3.ZipkinConfig_HTTP_JSON,
	}

	// 2. Marshal it into an 'Any' protobuf message
	zipkinTypedConfig, err := anypb.New(zipkinConfig)
	if err != nil {
		panic(err) // Handle error appropriately
	}

	grpcAccessLogConfig := &accessloggrpc.HttpGrpcAccessLogConfig{
		CommonConfig: &accessloggrpc.CommonGrpcAccessLogConfig{
			// Ein Name für diesen Log-Stream (kannst du im Go-Server filtern)
			LogName:             "faas_access_log",
			TransportApiVersion: corev3.ApiVersion_V3,
			GrpcService: &corev3.GrpcService{
				TargetSpecifier: &corev3.GrpcService_EnvoyGrpc_{
					EnvoyGrpc: &corev3.GrpcService_EnvoyGrpc{
						// WICHTIG: Das muss der Cluster-Name sein, unter dem Envoy
						// deinen Go-Server (Control Plane) erreicht.
						// Meistens "xds_cluster" oder "control-plane".
						ClusterName: "xds_cluster",
					},
				},
			},
		},
		// Optional: Zusätzliche Header, die im Log erscheinen sollen
		AdditionalRequestHeadersToLog:  []string{"x-request-id", "x-faas-container-id"},
		AdditionalResponseHeadersToLog: []string{"content-type", "x-response-duration", "my-custom-header"},
	}

	// Verpacken in "Any"
	accessLogTyped, err := anypb.New(grpcAccessLogConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal Access Log Config: %v", err)
	}

	hcmConfig := &hcm.HttpConnectionManager{

		StatPrefix: listenerConfiguration.ListenerName,

		GenerateRequestId: &wrapperspb.BoolValue{Value: true},

		AccessLog: []*accesslogv3.AccessLog{
			{
				Name: "envoy.access_loggers.file",
				ConfigType: &accesslogv3.AccessLog_TypedConfig{
					TypedConfig: accessLogTyped,
				},
			},
		},

		Tracing: &hcm.HttpConnectionManager_Tracing{
			Provider: &tracev3.Tracing_Http{
				Name: "envoy.tracers.zipkin",
				ConfigType: &tracev3.Tracing_Http_TypedConfig{
					TypedConfig: zipkinTypedConfig,
				},
			},
		},

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

	//Listener
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

	for _, route := range routes {
		ListenerUsingClusters[route.Prefix] = []string{
			route.ClusterToUse}
	}

	updateFullClusterBlock()

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

func getCurrentListenerConfigurations() []*Config {
	return CurrentListenerConfigurations
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

func createRouteComponent(prefix string, routeToCluster string, regexRewrite *RegexRewriteDefinition) *routev3.Route {
	// Gemeinsame Retry-Policy definieren
	retryPolicy := &routev3.RetryPolicy{
		RetryOn:    "5xx,connect-failure,refused-stream",
		NumRetries: &wrapperspb.UInt32Value{Value: 10},
		RetryBackOff: &routev3.RetryPolicy_RetryBackOff{
			BaseInterval: durationpb.New(50 * time.Millisecond), // Etwas Puffer für den Cluster-Warmup
			MaxInterval:  durationpb.New(200 * time.Millisecond),
		},
	}

	routeAction := &routev3.RouteAction{
		ClusterSpecifier: &routev3.RouteAction_Cluster{
			Cluster: routeToCluster,
		},
		RetryPolicy: retryPolicy, // Policy wird hier für beide Fälle zugewiesen
		Timeout:     durationpb.New(300 * time.Second),
	}

	// Falls Rewrite-Logik benötigt wird, hinzufügen
	if regexRewrite != nil {
		routeAction.RegexRewrite = &matcherv3.RegexMatchAndSubstitute{
			Pattern: &matcherv3.RegexMatcher{
				Regex: regexRewrite.Regex,
			},
			Substitution: regexRewrite.Substitution,
		}
	}

	return &routev3.Route{
		Match: &routev3.RouteMatch{
			PathSpecifier: &routev3.RouteMatch_Prefix{Prefix: prefix},
		},
		Action: &routev3.Route_Route{
			Route: routeAction,
		},
	}
}

//Functions to push data to Nginx

func pushClusterBlockList() error {
	time.Sleep(10 * time.Millisecond)
	payload := struct {
		Blocked []string `json:"blocked"`
	}{
		Blocked: ClusterBlockList,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %v", err)
	}
	println("JSON Data: ", string(jsonData))
	resp, err := http.Post("http://gate:99/admin/config", "application/json",
		bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to make request to nginx: %v", err)
	}
	defer resp.Body.Close() // Always close response body

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}
	return nil
}

func getKeysForValue(data map[string][]string, target string) []string {
	var matchingKeys []string

	for key, list := range data {
		for _, item := range list {
			if item == target {
				matchingKeys = append(matchingKeys, key)
				break
			}
		}
	}
	return matchingKeys
}

func removeString(list []string, target string) []string {
	for i, item := range list {
		println("REMOVE")
		println(item, "  ", target)
		if item == target {
			return append(list[:i], list[i+1:]...)
		}
	}
	return list
}

func updateBlockList(clusterName string, routesThere bool) error {
	keys := getKeysForValue(ListenerUsingClusters, clusterName)
	for _, key := range keys {
		println("keys: ", key)
	}
	if len(keys) > 0 {
		if !routesThere {
			ClusterBlockList = append(ClusterBlockList, keys...)
		} else if len(ClusterBlockList) > 0 {
			for _, key := range keys {
				print(key)
				ClusterBlockList = removeString(ClusterBlockList, key)
			}
		}
	}
	println("Update Block List: ", ClusterBlockList)
	println(clusterName)
	println(routesThere)
	formattedList := strings.Join(ClusterBlockList, ", ")
	fmt.Println("\n--- Comma Separated ---")
	fmt.Println(formattedList)
	return nil
}

func updateFullClusterBlock() {
	for _, cluster := range CurrentClusters {
		clusterName := cluster.Name
		anyRoutes := false
		for _, endpoint := range cluster.LoadAssignment.Endpoints {
			if endpoint.LbEndpoints != nil && len(endpoint.LbEndpoints) > 0 {
				anyRoutes = true
			}
		}
		err := updateBlockList(clusterName, anyRoutes)
		if err != nil {
			return
		}
	}
}
