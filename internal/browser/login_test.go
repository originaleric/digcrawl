package browser

import "testing"

func TestThreadsLoginSpecUsesCurrentDomain(t *testing.T) {
	t.Parallel()

	spec, err := LoginSpecFor("threads")
	if err != nil {
		t.Fatal(err)
	}
	if spec.IndexURL != "https://www.threads.com" || spec.CookieDomain != "www.threads.com" {
		t.Fatalf("unexpected threads login spec: %+v", spec)
	}
}
