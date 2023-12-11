package main

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIsVaildPod(t *testing.T) {
	pods := &v1.PodList{
		Items: []v1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{
					UID: "123",
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					UID: "456",
				},
			},
		},
	}

	cases := []struct {
		name     string
		expected bool
	}{
		{
			name:     "123",
			expected: true,
		},
		{
			name:     "789",
			expected: false,
		},
	}

	for _, c := range cases {
		if got := isVaildPod(c.name, pods); got != c.expected {
			t.Errorf("isVaildPod(%q) == %v, want %v", c.name, got, c.expected)
		}
	}
}
