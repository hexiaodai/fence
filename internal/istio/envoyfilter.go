package istio

import (
	"fmt"
	"strconv"
	"strings"

	"google.golang.org/protobuf/types/known/structpb"
	"istio.io/api/networking/v1alpha3"
	networkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	corev1 "k8s.io/api/core/v1"
)

var (
	fenceProxyMatch = &v1alpha3.EnvoyFilter_ProxyMatch{Metadata: map[string]string{"FENCE_APP": "FENCE_PROXY"}}
	emptyProxyMatch = &v1alpha3.EnvoyFilter_ProxyMatch{}

	allowAnyVhost   = &v1alpha3.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{Name: "allow_any"}
	fenceProxyVhost = &v1alpha3.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{Name: "fence_proxy"}
)

func MergeFenceProxyEnvoyFilter(envoyFilter *v1alpha3.EnvoyFilter, svc *corev1.Service) {
	for _, port := range svc.Spec.Ports {
		if port.Protocol != corev1.ProtocolTCP {
			continue
		}
		if !alreadyAllowAnyVirtualHost(envoyFilter, port) {
			envoyFilter.ConfigPatches = append(envoyFilter.ConfigPatches, generateVirtualHost(port, emptyProxyMatch, allowAnyVhost))
		}
		if !alreadyVirtualHost(envoyFilter, port) {
			envoyFilter.ConfigPatches = append(envoyFilter.ConfigPatches, generateVirtualHost(port, fenceProxyMatch, fenceProxyVhost))
		}
		if !alreadyRouteConfigUration(envoyFilter, port) {
			envoyFilter.ConfigPatches = append(envoyFilter.ConfigPatches, generateRouteConfigUration(port))
		}
		if !alreadyAllowAnyNewRouteConfigUration(envoyFilter, port) {
			envoyFilter.ConfigPatches = append(envoyFilter.ConfigPatches, generateRouteConfigUrationAllowAnyNew(port))
		}
		if !alreadyHttpFilter(envoyFilter, port) {
			envoyFilter.ConfigPatches = append(envoyFilter.ConfigPatches, generateHttpFilter(port))
		}
		if !alreadyHttpRoute(envoyFilter, port) {
			envoyFilter.ConfigPatches = append(envoyFilter.ConfigPatches, generateHttpRoute(port, fenceProxyVhost))
		}
	}
}

func generateVirtualHost(svcPort corev1.ServicePort, proxyMatch *v1alpha3.EnvoyFilter_ProxyMatch, vhost *v1alpha3.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch) *v1alpha3.EnvoyFilter_EnvoyConfigObjectPatch {
	config := &v1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
		ApplyTo: v1alpha3.EnvoyFilter_VIRTUAL_HOST,
		Match: &v1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
			Context: v1alpha3.EnvoyFilter_SIDECAR_OUTBOUND,
			Proxy:   proxyMatch,
			ObjectTypes: &v1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
				RouteConfiguration: &v1alpha3.EnvoyFilter_RouteConfigurationMatch{
					Name:  strconv.Itoa(int(svcPort.Port)),
					Vhost: vhost,
				},
			},
		},
		Patch: &v1alpha3.EnvoyFilter_Patch{
			Operation: v1alpha3.EnvoyFilter_Patch_REMOVE,
			Value:     &structpb.Struct{},
		},
	}
	return config
}

func generateRouteConfigUration(svcPort corev1.ServicePort) *v1alpha3.EnvoyFilter_EnvoyConfigObjectPatch {
	config := &v1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
		ApplyTo: v1alpha3.EnvoyFilter_ROUTE_CONFIGURATION,
		Match: &v1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
			Context: v1alpha3.EnvoyFilter_SIDECAR_OUTBOUND,
			ObjectTypes: &v1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
				RouteConfiguration: &v1alpha3.EnvoyFilter_RouteConfigurationMatch{
					Name: strconv.Itoa(int(svcPort.Port)),
				},
			},
		},
		Patch: &v1alpha3.EnvoyFilter_Patch{
			Operation: v1alpha3.EnvoyFilter_Patch_MERGE,
			Value: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"request_headers_to_add": {
						Kind: &structpb.Value_ListValue{
							ListValue: &structpb.ListValue{
								Values: []*structpb.Value{
									{
										Kind: &structpb.Value_StructValue{
											StructValue: &structpb.Struct{
												Fields: map[string]*structpb.Value{
													"append": {
														Kind: &structpb.Value_BoolValue{BoolValue: true},
													},
													"header": {
														Kind: &structpb.Value_StructValue{
															StructValue: &structpb.Struct{
																Fields: map[string]*structpb.Value{
																	"key":   {Kind: &structpb.Value_StringValue{StringValue: "Fence-Orig-Dest"}},
																	"value": {Kind: &structpb.Value_StringValue{StringValue: "%DOWNSTREAM_LOCAL_ADDRESS%"}},
																},
															},
														},
													},
												},
											},
										},
									},
								},
							},
						}},
					"virtual_hosts": {
						Kind: &structpb.Value_ListValue{
							ListValue: &structpb.ListValue{
								Values: []*structpb.Value{
									{
										Kind: &structpb.Value_StructValue{
											StructValue: &structpb.Struct{
												Fields: map[string]*structpb.Value{
													"domains": {
														Kind: &structpb.Value_ListValue{
															ListValue: &structpb.ListValue{
																Values: []*structpb.Value{
																	{
																		Kind: &structpb.Value_StringValue{StringValue: "*"},
																	},
																},
															},
														},
													},
													"name": {
														Kind: &structpb.Value_StringValue{StringValue: "fence_proxy"},
													},
													"routes": {
														Kind: &structpb.Value_ListValue{
															ListValue: &structpb.ListValue{
																Values: []*structpb.Value{
																	{
																		Kind: &structpb.Value_StructValue{
																			StructValue: &structpb.Struct{
																				Fields: map[string]*structpb.Value{
																					"match": {
																						Kind: &structpb.Value_StructValue{
																							StructValue: &structpb.Struct{
																								Fields: map[string]*structpb.Value{
																									"headers": {
																										Kind: &structpb.Value_ListValue{
																											ListValue: &structpb.ListValue{
																												Values: []*structpb.Value{
																													{
																														Kind: &structpb.Value_StructValue{
																															StructValue: &structpb.Struct{
																																Fields: map[string]*structpb.Value{
																																	"name": {Kind: &structpb.Value_StringValue{
																																		StringValue: ":authority"}},
																																	"string_match": {
																																		Kind: &structpb.Value_StructValue{
																																			StructValue: &structpb.Struct{
																																				Fields: map[string]*structpb.Value{
																																					"safe_regex": {
																																						Kind: &structpb.Value_StructValue{
																																							StructValue: &structpb.Struct{
																																								Fields: map[string]*structpb.Value{
																																									"regex": {
																																										Kind: &structpb.Value_StringValue{
																																											StringValue: `^(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)(?::([1-9]|[1-9]\d{1,3}|[1-5]\d{4}|6[0-5][0-5][0-3][0-5]))?$`,
																																										},
																																									},
																																									"google_re2": {Kind: &structpb.Value_StructValue{StructValue: &structpb.Struct{Fields: map[string]*structpb.Value{}}}},
																																								},
																																							},
																																						},
																																					},
																																				},
																																			},
																																		},
																																	},
																																},
																															},
																														},
																													},
																												},
																											},
																										},
																									},
																									"prefix": {
																										Kind: &structpb.Value_StringValue{StringValue: "/"},
																									},
																								},
																							},
																						},
																					},
																					"route": {
																						Kind: &structpb.Value_StructValue{
																							StructValue: &structpb.Struct{
																								Fields: map[string]*structpb.Value{
																									"cluster": {Kind: &structpb.Value_StringValue{StringValue: "PassthroughCluster"}},
																									"timeout": {Kind: &structpb.Value_StringValue{StringValue: "0s"}},
																								},
																							},
																						},
																					},
																				},
																			},
																		},
																	},
																	{
																		Kind: &structpb.Value_StructValue{
																			StructValue: &structpb.Struct{
																				Fields: map[string]*structpb.Value{
																					"match": {
																						Kind: &structpb.Value_StructValue{
																							StructValue: &structpb.Struct{
																								Fields: map[string]*structpb.Value{
																									"prefix": {Kind: &structpb.Value_StringValue{StringValue: "/"}},
																								},
																							},
																						},
																					},
																					"route": {
																						Kind: &structpb.Value_StructValue{
																							StructValue: &structpb.Struct{
																								Fields: map[string]*structpb.Value{
																									"cluster": {Kind: &structpb.Value_StringValue{StringValue: "outbound|80||fence-proxy.fence.svc.cluster.local"}},
																									"timeout": {Kind: &structpb.Value_StringValue{StringValue: "0s"}},
																								},
																							},
																						},
																					},
																				},
																			},
																		},
																	},
																},
															},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	return config
}

func generateRouteConfigUrationAllowAnyNew(svcPort corev1.ServicePort) *v1alpha3.EnvoyFilter_EnvoyConfigObjectPatch {
	config := &v1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
		ApplyTo: v1alpha3.EnvoyFilter_ROUTE_CONFIGURATION,
		Match: &v1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
			Context: v1alpha3.EnvoyFilter_SIDECAR_OUTBOUND,
			Proxy:   fenceProxyMatch,
			ObjectTypes: &v1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
				RouteConfiguration: &v1alpha3.EnvoyFilter_RouteConfigurationMatch{
					Name: strconv.Itoa(int(svcPort.Port)),
				},
			},
		},
		Patch: &v1alpha3.EnvoyFilter_Patch{
			Operation: v1alpha3.EnvoyFilter_Patch_MERGE,
			Value: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"virtual_hosts": {
						Kind: &structpb.Value_ListValue{
							ListValue: &structpb.ListValue{
								Values: []*structpb.Value{
									{
										Kind: &structpb.Value_StructValue{
											StructValue: &structpb.Struct{
												Fields: map[string]*structpb.Value{
													"domains": {
														Kind: &structpb.Value_ListValue{
															ListValue: &structpb.ListValue{
																Values: []*structpb.Value{
																	{
																		Kind: &structpb.Value_StringValue{StringValue: "*"},
																	},
																},
															},
														},
													},
													"name": {
														Kind: &structpb.Value_StringValue{StringValue: "allow_any_new"},
													},
													"routes": {
														Kind: &structpb.Value_ListValue{
															ListValue: &structpb.ListValue{
																Values: []*structpb.Value{
																	{
																		Kind: &structpb.Value_StructValue{
																			StructValue: &structpb.Struct{
																				Fields: map[string]*structpb.Value{
																					"match": {
																						Kind: &structpb.Value_StructValue{
																							StructValue: &structpb.Struct{
																								Fields: map[string]*structpb.Value{
																									"prefix": {
																										Kind: &structpb.Value_StringValue{StringValue: "/"},
																									},
																								},
																							},
																						},
																					},
																					"route": {
																						Kind: &structpb.Value_StructValue{
																							StructValue: &structpb.Struct{
																								Fields: map[string]*structpb.Value{
																									"cluster": {Kind: &structpb.Value_StringValue{StringValue: "PassthroughCluster"}},
																									"timeout": {Kind: &structpb.Value_StringValue{StringValue: "0s"}},
																								},
																							},
																						},
																					},
																				},
																			},
																		},
																	},
																},
															},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	return config
}

func generateHttpFilter(svcPort corev1.ServicePort) *v1alpha3.EnvoyFilter_EnvoyConfigObjectPatch {
	config := &v1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
		ApplyTo: v1alpha3.EnvoyFilter_HTTP_FILTER,
		Match: &v1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
			Context: v1alpha3.EnvoyFilter_SIDECAR_OUTBOUND,
			ObjectTypes: &v1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
				Listener: &v1alpha3.EnvoyFilter_ListenerMatch{
					FilterChain: &v1alpha3.EnvoyFilter_ListenerMatch_FilterChainMatch{
						Filter: &v1alpha3.EnvoyFilter_ListenerMatch_FilterMatch{
							Name: "envoy.filters.network.http_connection_manager",
							SubFilter: &v1alpha3.EnvoyFilter_ListenerMatch_SubFilterMatch{
								Name: "envoy.filters.http.router",
							},
						},
					},
					Name: fmt.Sprintf("0.0.0.0_%v", svcPort.Port),
				},
			},
		},
		Patch: &v1alpha3.EnvoyFilter_Patch{
			Operation: v1alpha3.EnvoyFilter_Patch_INSERT_BEFORE,
			Value: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"name": {
						Kind: &structpb.Value_StringValue{StringValue: "envoy.filters.http.lua"},
					},
					"typed_config": {
						Kind: &structpb.Value_StructValue{
							StructValue: &structpb.Struct{
								Fields: map[string]*structpb.Value{
									"@type":       {Kind: &structpb.Value_StringValue{StringValue: "type.googleapis.com/envoy.extensions.filters.http.lua.v3.Lua"}},
									"inline_code": {Kind: &structpb.Value_StringValue{StringValue: "-- place holder"}},
									"source_codes": {
										Kind: &structpb.Value_StructValue{
											StructValue: &structpb.Struct{
												Fields: map[string]*structpb.Value{
													"add.lua": {
														Kind: &structpb.Value_StructValue{
															StructValue: &structpb.Struct{
																Fields: map[string]*structpb.Value{
																	"inline_string": {
																		Kind: &structpb.Value_StringValue{
																			StringValue: "function envoy_on_request(request_handle) request_handle:headers():replace(\"Fence-Source-Ns\", os.getenv(\"POD_NAMESPACE\")) end",
																		},
																	},
																},
															},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	return config
}

func generateHttpRoute(svcPort corev1.ServicePort, vhost *v1alpha3.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch) *v1alpha3.EnvoyFilter_EnvoyConfigObjectPatch {
	config := &v1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
		ApplyTo: v1alpha3.EnvoyFilter_HTTP_ROUTE,
		Match: &v1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
			Context: v1alpha3.EnvoyFilter_SIDECAR_OUTBOUND,
			ObjectTypes: &v1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
				RouteConfiguration: &v1alpha3.EnvoyFilter_RouteConfigurationMatch{
					Name:  strconv.Itoa(int(svcPort.Port)),
					Vhost: vhost,
				},
			},
		},
		Patch: &v1alpha3.EnvoyFilter_Patch{
			Operation: v1alpha3.EnvoyFilter_Patch_MERGE,
			Value: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"typed_per_filter_config": {
						Kind: &structpb.Value_StructValue{
							StructValue: &structpb.Struct{
								Fields: map[string]*structpb.Value{
									"envoy.filters.http.lua": {
										Kind: &structpb.Value_StructValue{
											StructValue: &structpb.Struct{
												Fields: map[string]*structpb.Value{
													"@type": {Kind: &structpb.Value_StringValue{StringValue: "type.googleapis.com/envoy.extensions.filters.http.lua.v3.LuaPerRoute"}},
													"name":  {Kind: &structpb.Value_StringValue{StringValue: "add.lua"}},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	return config
}

func alreadyAllowAnyVirtualHost(envoyFilter *v1alpha3.EnvoyFilter, svcPort corev1.ServicePort) bool {
	for _, patche := range envoyFilter.ConfigPatches {
		if patche.ApplyTo == v1alpha3.EnvoyFilter_VIRTUAL_HOST &&
			patche.Match.GetRouteConfiguration().GetName() == strconv.Itoa(int(svcPort.Port)) &&
			patche.Match.GetRouteConfiguration().GetVhost().Name == allowAnyVhost.Name {
			return true
		}
	}
	return false
}

func alreadyVirtualHost(envoyFilter *v1alpha3.EnvoyFilter, svcPort corev1.ServicePort) bool {
	for _, patche := range envoyFilter.ConfigPatches {
		if patche.ApplyTo == v1alpha3.EnvoyFilter_VIRTUAL_HOST && patche.Match.GetRouteConfiguration().GetName() == strconv.Itoa(int(svcPort.Port)) {
			return true
		}
	}
	return false
}

func alreadyRouteConfigUration(envoyFilter *v1alpha3.EnvoyFilter, svcPort corev1.ServicePort) bool {
	for _, patche := range envoyFilter.ConfigPatches {
		if patche.ApplyTo == v1alpha3.EnvoyFilter_ROUTE_CONFIGURATION && patche.Match.GetRouteConfiguration().GetName() == strconv.Itoa(int(svcPort.Port)) {
			return true
		}
	}
	return false
}

func alreadyAllowAnyNewRouteConfigUration(envoyFilter *v1alpha3.EnvoyFilter, svcPort corev1.ServicePort) bool {
	for _, patche := range envoyFilter.ConfigPatches {
		if patche.ApplyTo == v1alpha3.EnvoyFilter_ROUTE_CONFIGURATION && patche.Match.GetRouteConfiguration().GetName() == strconv.Itoa(int(svcPort.Port)) {
			vh, ok := patche.Patch.GetValue().GetFields()["virtual_hosts"]
			if !ok {
				return false
			}
			for _, value := range vh.GetListValue().GetValues() {
				name, ok := value.GetStructValue().GetFields()["name"]
				if !ok {
					return false
				}
				if name.GetStringValue() == "allow_any_new" {
					return true
				}
			}
		}
	}
	return false
}

func alreadyHttpFilter(envoyFilter *v1alpha3.EnvoyFilter, svcPort corev1.ServicePort) bool {
	for _, patche := range envoyFilter.ConfigPatches {
		if patche.ApplyTo == v1alpha3.EnvoyFilter_HTTP_FILTER &&
			patche.Match.GetListener().GetFilterChain().GetFilter().GetName() == fmt.Sprintf("0.0.0.0_%v", svcPort.Port) {
			return true
		}
	}
	return false
}

func alreadyHttpRoute(envoyFilter *v1alpha3.EnvoyFilter, svcPort corev1.ServicePort) bool {
	for _, patche := range envoyFilter.ConfigPatches {
		if patche.ApplyTo == v1alpha3.EnvoyFilter_HTTP_ROUTE && patche.Match.GetRouteConfiguration().GetName() == strconv.Itoa(int(svcPort.Port)) {
			return true
		}
	}
	return false
}

func AddExternalServiceToRouteConfigUration(authority string, envoyFilter *networkingv1alpha3.EnvoyFilter) {
	destParts := strings.Split(authority, ":")
	destSvc, destPort := destParts[0], "80"
	if len(destParts) == 2 {
		destPort = destParts[1]
	}

	for _, patche := range envoyFilter.Spec.ConfigPatches {
		if patche.ApplyTo == v1alpha3.EnvoyFilter_ROUTE_CONFIGURATION && patche.Match.GetRouteConfiguration().GetName() == destPort {
			newvh := &structpb.Value{
				Kind: &structpb.Value_StructValue{
					StructValue: &structpb.Struct{
						Fields: map[string]*structpb.Value{
							"domains": {
								Kind: &structpb.Value_ListValue{
									ListValue: &structpb.ListValue{
										Values: []*structpb.Value{
											{
												Kind: &structpb.Value_StringValue{StringValue: destSvc},
											},
										},
									},
								},
							},
							"name": {Kind: &structpb.Value_StringValue{StringValue: destSvc}},
							"routes": {
								Kind: &structpb.Value_ListValue{
									ListValue: &structpb.ListValue{
										Values: []*structpb.Value{
											{
												Kind: &structpb.Value_StructValue{
													StructValue: &structpb.Struct{
														Fields: map[string]*structpb.Value{
															"match": {
																Kind: &structpb.Value_StructValue{
																	StructValue: &structpb.Struct{
																		Fields: map[string]*structpb.Value{
																			"prefix": {Kind: &structpb.Value_StringValue{StringValue: "/"}},
																		},
																	},
																},
															},
															"route": {
																Kind: &structpb.Value_StructValue{
																	StructValue: &structpb.Struct{
																		Fields: map[string]*structpb.Value{
																			"cluster": {Kind: &structpb.Value_StringValue{StringValue: "PassthroughCluster"}},
																		},
																	},
																},
															},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			}
			if _, ok := patche.Patch.Value.Fields["virtual_hosts"]; ok && !alreadyExistVirtualHosts(patche.Patch.Value.Fields["virtual_hosts"], destSvc) {
				patche.Patch.Value.Fields["virtual_hosts"].GetListValue().Values = append(patche.Patch.Value.Fields["virtual_hosts"].GetListValue().Values, newvh)
			}
			return
		}
	}
}

func alreadyExistVirtualHosts(vhs *structpb.Value, domain string) bool {
	for _, vhItem := range vhs.GetListValue().Values {
		domains, ok := vhItem.GetStructValue().Fields["domains"]
		if !ok {
			continue
		}
		for _, domainValue := range domains.GetListValue().Values {
			if domainValue.GetStringValue() == domain {
				return true
			}
		}
	}
	return false
}
