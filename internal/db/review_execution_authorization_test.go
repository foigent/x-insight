package db

import (
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/workspace"
)

func TestExecutionContentOriginsCoverEveryAsOf(t *testing.T) {
	a := time.Date(2026, 9, 23, 0, 0, 0, 123456000, time.UTC)
	b := a.Add(time.Microsecond)
	c := b.Add(time.Microsecond)
	pending := func(id string, at time.Time, content bool) *workspace.ResolvedExecutionContext {
		v := &workspace.ResolvedExecutionContext{Context: workspace.ExecutionContext{ExecutionID: id, ExecutionAsOf: at}}
		if content {
			v.Contents = []workspace.ContentChoice{{}}
		}
		return v
	}
	for _, tt := range []struct {
		name           string
		links, origins []time.Time
		pending        *workspace.ResolvedExecutionContext
		want           bool
	}{
		{"empty", nil, nil, nil, false},
		{"durable exact origin", []time.Time{a}, []time.Time{a}, nil, true},
		{"all durable links", []time.Time{a, b}, []time.Time{b, a}, nil, true},
		{"uncovered durable link", []time.Time{a, b}, []time.Time{a}, nil, false},
		{"origin without link", nil, []time.Time{a}, nil, false},
		{"same instant different zone", []time.Time{a.In(time.FixedZone("other", 3600))}, []time.Time{a}, nil, true},
		{"new content origin", nil, nil, pending("execution", a, true), true},
		{"new resource origin", nil, nil, pending("execution", a, false), false},
		{"resource duplicate retained tuple", []time.Time{a}, []time.Time{a}, pending("execution", a, false), true},
		{"resource adds uncovered tuple", []time.Time{a}, []time.Time{a}, pending("execution", b, false), false},
		{"content adds covered tuple", []time.Time{a}, []time.Time{a}, pending("execution", b, true), true},
		{"content cannot bless another tuple", []time.Time{a, b}, []time.Time{a}, pending("execution", c, true), false},
		{"resource duplicate cannot hide uncovered old tuple", []time.Time{a, b}, []time.Time{a}, pending("execution", a, false), false},
		{"content duplicate cannot hide uncovered old tuple", []time.Time{a, b}, []time.Time{a}, pending("execution", a, true), false},
		{"different execution", []time.Time{a}, []time.Time{a}, pending("other", a, true), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := executionContentOriginsCover("execution", tt.links, tt.origins, tt.pending); got != tt.want {
				t.Fatalf("got %t, want %t", got, tt.want)
			}
		})
	}
}
