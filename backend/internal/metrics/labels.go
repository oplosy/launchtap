package metrics

import (
	"fmt"
	"regexp"
	"sort"

	"github.com/prometheus/client_golang/prometheus"
)

// unsafeLabelValue matches label values that must never reach a metrics
// endpoint: EVM addresses, 32-byte hashes, DIDs and URLs.
var unsafeLabelValue = regexp.MustCompile(`0x[0-9a-fA-F]{40}|0x[0-9a-fA-F]{64}|did:|://`)

// UnsafeLabels returns every `name{label="value"}` series in g whose label
// value looks like an address, hash, DID or URL. The result is sorted. It is a
// guard for tests: an empty result means every label comes from a safe set.
func UnsafeLabels(g prometheus.Gatherer) ([]string, error) {
	families, err := g.Gather()
	if err != nil {
		return nil, fmt.Errorf("gather metrics: %w", err)
	}

	var unsafe []string
	for _, family := range families {
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				if unsafeLabelValue.MatchString(label.GetValue()) {
					unsafe = append(unsafe, fmt.Sprintf("%s{%s=%q}", family.GetName(), label.GetName(), label.GetValue()))
				}
			}
		}
	}
	sort.Strings(unsafe)

	return unsafe, nil
}
