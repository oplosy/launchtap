package metrics

import "testing"

func TestWebClientSeries(t *testing.T) {
	t.Parallel()

	reg := NewRegistry(ComponentAPI)
	api := NewAPI(reg)
	api.ClientError("window_error", "token", "TypeError")
	api.ClientError("window_error", "token", "TypeError")
	api.ClientVital("LCP", "home", 1500)
	api.ClientVital("CLS", "home", 0.12)

	unsafe, err := UnsafeLabels(reg)
	if err != nil {
		t.Fatal(err)
	}
	if len(unsafe) != 0 {
		t.Fatalf("unsafe labels: %v", unsafe)
	}
	if got := seriesValue(t, reg, "launchpad_web_client_errors_total", map[string]string{"source": "window_error", "route": "token", "class": "TypeError"}); got != 2 {
		t.Fatalf("client errors = %v, want 2", got)
	}
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	sums := map[string]float64{}
	for _, family := range families {
		for _, metric := range family.GetMetric() {
			if h := metric.GetHistogram(); h != nil && h.GetSampleCount() == 1 && len(metric.GetLabel()) > 0 {
				sums[family.GetName()] = h.GetSampleSum()
			}
		}
	}
	if sums["launchpad_web_vital_seconds"] != 1.5 {
		t.Fatalf("LCP sum = %v s, want 1.5 (milliseconds converted)", sums["launchpad_web_vital_seconds"])
	}
	if sums["launchpad_web_cls"] != 0.12 {
		t.Fatalf("CLS sum = %v, want 0.12 (unitless)", sums["launchpad_web_cls"])
	}
}
