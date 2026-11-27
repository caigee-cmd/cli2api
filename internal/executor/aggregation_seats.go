package executor

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

func (e ChatExecutor) pickSeats(requestID, prefer, providerFilter, regionFilter, publicModel string, allowed []string, excluded map[string]struct{}, eligible func(Item) bool, plan *AggregatePlan) (Item, error) {
	order := e.orderedSeats(plan, publicModel, allowed, excluded, eligible, len(excluded) == 0)
	var first error
	for _, seat := range order {
		child := planForSeat(plan, seat)
		item, err := e.pick(requestID, prefer, providerFilter, regionFilter, publicModel, allowed, excluded, eligible, child)
		if err == nil {
			return item, nil
		}
		if first == nil {
			first = err
		}
		if !seatMiss(err) {
			return Item{}, err
		}
	}
	if first != nil {
		return Item{}, first
	}
	return Item{}, noAccountsError("no accounts available")
}

func (e ChatExecutor) orderedSeats(plan *AggregatePlan, publicModel string, allowed []string, excluded map[string]struct{}, eligible func(Item) bool, advance bool) []accounts.RouteSeat {
	seats := append([]accounts.RouteSeat(nil), plan.Seats...)
	switch plan.Strategy {
	case accounts.AggregationFillFirst:
		sort.SliceStable(seats, func(i, j int) bool {
			if seats[i].Weight != seats[j].Weight {
				return seats[i].Weight > seats[j].Weight
			}
			return seats[i].ID < seats[j].ID
		})
	case accounts.AggregationLeastQuotaFirst, accounts.AggregationMostQuotaFirst:
		ready := e.readyFor(plan, publicModel, allowed, excluded, eligible)
		most := plan.Strategy == accounts.AggregationMostQuotaFirst
		sort.SliceStable(seats, func(i, j int) bool {
			left, leftOK := seatRatio(seats[i], ready)
			right, rightOK := seatRatio(seats[j], ready)
			if leftOK != rightOK {
				return leftOK
			}
			if leftOK && left != right {
				if most {
					return left > right
				}
				return left < right
			}
			return seats[i].Priority < seats[j].Priority
		})
	case accounts.AggregationFEFOStrategy:
		ready := e.readyFor(plan, publicModel, allowed, excluded, eligible)
		now := time.Now()
		sort.SliceStable(seats, func(i, j int) bool {
			left, leftOK := seatExpiry(seats[i], ready, publicModel, now)
			right, rightOK := seatExpiry(seats[j], ready, publicModel, now)
			if leftOK != rightOK {
				return leftOK
			}
			if leftOK && left != right {
				return left < right
			}
			return seats[i].Priority < seats[j].Priority
		})
	case accounts.AggregationRoundRobin:
		sort.SliceStable(seats, func(i, j int) bool { return seats[i].Priority < seats[j].Priority })
		ids := make([]string, len(seats))
		byID := make(map[string]accounts.RouteSeat, len(seats))
		for i, seat := range seats {
			ids[i] = seat.ID
			byID[seat.ID] = seat
		}
		if e.Pool != nil {
			ids = e.Pool.RotateIDs(plan.ID+"|seats", ids, advance)
		}
		rotated := make([]accounts.RouteSeat, 0, len(ids))
		for _, id := range ids {
			rotated = append(rotated, byID[id])
		}
		return rotated
	default:
		sort.SliceStable(seats, func(i, j int) bool {
			if seats[i].Priority != seats[j].Priority {
				return seats[i].Priority < seats[j].Priority
			}
			return seats[i].ID < seats[j].ID
		})
	}
	return seats
}

func (e ChatExecutor) readyFor(plan *AggregatePlan, publicModel string, allowed []string, excluded map[string]struct{}, eligible func(Item) bool) []Item {
	if e.Pool == nil {
		return nil
	}
	flat := *plan
	flat.Seats = nil
	return e.Pool.ReadyItems(e.routeQuery("", "", "", publicModel, allowed, excluded, eligible, &flat))
}

func planForSeat(parent *AggregatePlan, seat accounts.RouteSeat) *AggregatePlan {
	leaves := seatLeaves(seat)
	id := parent.ID
	strategy := accounts.AggregationPriority
	if seat.Kind == "group" {
		id = seat.GroupID
		strategy = seat.GroupStrategy
	}
	plan := NewAggregatePlan(accounts.Aggregation{
		ID: id, Strategy: strategy, AllowFallbacks: parent.AllowFallbacks, MaxAttempts: parent.MaxAttempts,
		ReasoningEffort: parent.ReasoningEffort, Members: leaves,
	})
	if seat.Kind == "group" && len(seat.GroupSeats) > 0 {
		plan.Seats = seat.GroupSeats
		plan.Strategy = seat.GroupStrategy
	}
	return plan
}

func seatLeaves(seat accounts.RouteSeat) []accounts.AggregationMember {
	if seat.Kind == "member" {
		return []accounts.AggregationMember{seat.Member}
	}
	if len(seat.GroupMembers) > 0 {
		return append([]accounts.AggregationMember(nil), seat.GroupMembers...)
	}
	var out []accounts.AggregationMember
	for _, child := range seat.GroupSeats {
		out = append(out, seatLeaves(child)...)
	}
	return out
}

func seatRatio(seat accounts.RouteSeat, ready []Item) (float64, bool) {
	plan := NewAggregatePlan(accounts.Aggregation{Members: seatLeaves(seat)})
	best := 0.0
	ok := false
	for _, item := range ready {
		if !plan.CanServeAccount(item) {
			continue
		}
		ratio, known := quotaRemainingRatio(item)
		if !known {
			continue
		}
		if !ok || ratio < best {
			best = ratio
			ok = true
		}
	}
	return best, ok
}

func seatExpiry(seat accounts.RouteSeat, ready []Item, publicModel string, now time.Time) (int64, bool) {
	plan := NewAggregatePlan(accounts.Aggregation{Members: seatLeaves(seat)})
	best := int64(0)
	ok := false
	for _, item := range ready {
		if !plan.CanServeAccount(item) {
			continue
		}
		expiry, depleted := fefoExpiry(item, publicModel, now)
		if depleted || expiry == 0 {
			continue
		}
		if !ok || expiry < best {
			best = expiry
			ok = true
		}
	}
	return best, ok
}

func seatMiss(err error) bool {
	var execution *ExecutionError
	if !errors.As(err, &execution) {
		return false
	}
	switch execution.Classified.Code {
	case "model_not_available", "no_accounts_configured", "no_accounts", "rate_limit", "insufficient_quota":
		return true
	default:
		return strings.Contains(execution.Classified.Message, "no accounts")
	}
}

// PreviewAttempt is one concrete model in the order a request would try it.
type PreviewAttempt struct {
	ID       string `json:"id"`
	Model    string `json:"model"`
	Provider string `json:"provider"`
	Region   string `json:"region,omitempty"`
	Status   string `json:"status"`
	Reason   string `json:"reason,omitempty"`
}

// ExplainSeats lists leaf members in seat order and marks leaves that have no
// ready account.
func ExplainSeats(pool *Pool, plan *AggregatePlan) []PreviewAttempt {
	if plan == nil {
		return nil
	}
	var out []PreviewAttempt
	if len(plan.Seats) == 0 {
		for _, member := range plan.Members {
			out = append(out, attemptFor(pool, plan, member))
		}
		return out
	}
	var walk func(seats []accounts.RouteSeat)
	walk = func(seats []accounts.RouteSeat) {
		for _, seat := range seats {
			switch seat.Kind {
			case "member":
				out = append(out, attemptFor(pool, plan, seat.Member))
			case "group":
				if len(seat.GroupSeats) > 0 {
					walk(seat.GroupSeats)
					continue
				}
				for _, member := range seat.GroupMembers {
					out = append(out, attemptFor(pool, plan, member))
				}
			}
		}
	}
	walk(plan.Seats)
	return out
}

func attemptFor(pool *Pool, plan *AggregatePlan, member accounts.AggregationMember) PreviewAttempt {
	attempt := PreviewAttempt{
		ID: member.Provider + "/" + member.Model, Model: member.Model, Provider: member.Provider, Region: member.Region, Status: "waiting",
	}
	if pool == nil {
		return attempt
	}
	one := NewAggregatePlan(accounts.Aggregation{ID: plan.ID, Strategy: accounts.AggregationPriority, Members: []accounts.AggregationMember{member}})
	if len(pool.ReadyItems(RouteQuery{PublicModel: plan.ID, ModelResolver: one.resolver()})) == 0 {
		attempt.Status = "skipped"
		attempt.Reason = "no ready account"
	}
	return attempt
}
