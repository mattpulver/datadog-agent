// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

// Package stats contains the logic to process APM stats.
package stats

import (
	"hash/fnv"
	"sort"
	"strconv"
	"strings"

	pb "github.com/DataDog/datadog-agent/pkg/proto/pbgo/trace"
	"github.com/DataDog/datadog-agent/pkg/trace/log"
	"github.com/DataDog/datadog-agent/pkg/trace/traceutil"
	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/grpc/codes"
)

const (
	tagSynthetics  = "synthetics"
	tagSpanKind    = "span.kind"
	tagBaseService = "_dd.base_service"
)

// Aggregation contains all the dimension on which we aggregate statistics.
type Aggregation struct {
	BucketsAggregationKey
	PayloadAggregationKey
}

// BucketsAggregationKey specifies the key by which a bucket is aggregated.
type BucketsAggregationKey struct {
	Service        string
	Name           string
	Resource       string
	Type           string
	SpanKind       string
	StatusCode     uint32
	Synthetics     bool
	PeerTagsHash   uint64
	IsTraceRoot    pb.Trilean
	GRPCStatusCode string
}

// PayloadAggregationKey specifies the key by which a payload is aggregated.
type PayloadAggregationKey struct {
	Env          string
	Hostname     string
	Version      string
	ContainerID  string
	GitCommitSha string
	ImageTag     string
}

func getStatusCode(meta map[string]string, metrics map[string]float64) uint32 {
	code, ok := metrics[traceutil.TagStatusCode]
	if ok {
		// only 7.39.0+, for lesser versions, always use Meta
		return uint32(code)
	}
	strC := meta[traceutil.TagStatusCode]
	if strC == "" {
		return 0
	}
	c, err := strconv.ParseUint(strC, 10, 32)
	if err != nil {
		log.Debugf("Invalid status code %s. Using 0.", strC)
		return 0
	}
	return uint32(c)
}

// NewAggregationFromSpan creates a new aggregation from the provided span and env
func NewAggregationFromSpan(s *StatSpan, origin string, aggKey PayloadAggregationKey) Aggregation {
	synthetics := strings.HasPrefix(origin, tagSynthetics)
	var isTraceRoot pb.Trilean
	if s.parentID == 0 {
		isTraceRoot = pb.Trilean_TRUE
	} else {
		isTraceRoot = pb.Trilean_FALSE
	}
	agg := Aggregation{
		PayloadAggregationKey: aggKey,
		BucketsAggregationKey: BucketsAggregationKey{
			Resource:       s.resource,
			Service:        s.service,
			Name:           s.name,
			SpanKind:       s.spanKind,
			Type:           s.typ,
			StatusCode:     s.statusCode,
			Synthetics:     synthetics,
			IsTraceRoot:    isTraceRoot,
			GRPCStatusCode: s.grpcStatusCode,
			PeerTagsHash:   peerTagsHash(s.matchingPeerTags),
		},
	}
	return agg
}

func peerTagsHash(tags []string) uint64 {
	if len(tags) == 0 {
		return 0
	}
	if !sort.StringsAreSorted(tags) {
		sort.Strings(tags)
	}
	h := fnv.New64a()
	for i, t := range tags {
		if i > 0 {
			h.Write([]byte{0})
		}
		h.Write([]byte(t))
	}
	return h.Sum64()
}

// NewAggregationFromGroup gets the Aggregation key of grouped stats.
func NewAggregationFromGroup(g *pb.ClientGroupedStats) Aggregation {
	return Aggregation{
		BucketsAggregationKey: BucketsAggregationKey{
			Resource:       g.Resource,
			Service:        g.Service,
			Name:           g.Name,
			SpanKind:       g.SpanKind,
			StatusCode:     g.HTTPStatusCode,
			Synthetics:     g.Synthetics,
			PeerTagsHash:   peerTagsHash(g.PeerTags),
			IsTraceRoot:    g.IsTraceRoot,
			GRPCStatusCode: g.GRPCStatusCode,
		},
	}
}

func getGRPCStatusCode(meta map[string]string, metrics map[string]float64) string {
	// List of possible keys to check in order
	metaKeys := []string{"rpc.grpc.status_code", "grpc.code", "rpc.grpc.status.code", "grpc.status.code"}

	for _, key := range metaKeys {
		if strC, exists := meta[key]; exists && strC != "" {
			c, err := strconv.ParseUint(strC, 10, 32)
			if err == nil {
				return strconv.FormatUint(c, 10)
			}
			strCUpper := strings.ToUpper(strC)
			if strCUpper == "CANCELED" || strCUpper == "CANCELLED" { // the rpc code google api checks for "CANCELLED" but we receive "Canceled" from upstream
				return strconv.FormatInt(int64(codes.Canceled), 10)
			}

			// If not integer, check for valid gRPC status string
			if codeNum, found := code.Code_value[strCUpper]; found {
				return strconv.Itoa(int(codeNum))
			}

			return ""
		}
	}

	for _, key := range metaKeys { // metaKeys are the same keys we check for in metrics
		if code, ok := metrics[key]; ok {
			return strconv.FormatUint(uint64(code), 10)
		}
	}

	return ""
}
