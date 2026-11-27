package accounts

import "testing"

func TestPipelineKeywordSelectsPathAndKeepsOrder(t *testing.T) {
	doc := `{
	  "version": 1,
	  "steps": [
	    {"id":"b","kind":"branch","paths":[
	      {"id":"plan","label":"Plan","keywords":["plan"],"steps":[
	        {"id":"z","kind":"member","provider":"qoder","model":"model-z","account_id":"acc-z","weight":10},
	        {"id":"a","kind":"member","provider":"workbuddy","model":"model-a","weight":20}
	      ]},
	      {"id":"def","label":"Default","fallback":true,"steps":[
	        {"id":"d","kind":"member","provider":"trae","model":"model-d"}
	      ]}
	    ]}
	  ]
	}`
	agg, seats, path, err := CompilePipeline(Aggregation{ID: "mine", Document: doc, Strategy: AggregationPriority}, nil, "please plan this", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if path != "Plan" {
		t.Fatalf("path = %s", path)
	}
	if seats != nil {
		t.Fatalf("flat path should not become seats: %+v", seats)
	}
	if len(agg.Members) != 2 || agg.Members[0].Model != "model-z" || agg.Members[1].Model != "model-a" {
		t.Fatalf("members = %+v", agg.Members)
	}
	if agg.Members[1].Weight != 20 {
		t.Fatalf("weight = %d", agg.Members[1].Weight)
	}
	if agg.Members[0].AccountID != "acc-z" {
		t.Fatalf("account = %q", agg.Members[0].AccountID)
	}
}

func TestPipelineFallbackWhenClassifierMisses(t *testing.T) {
	doc := `{
	  "version": 1,
	  "steps": [
	    {"id":"b","kind":"branch","classifier":{"provider":"qoder","model":"small"},"paths":[
	      {"id":"plan","label":"Plan","intent":"plan","steps":[
	        {"id":"p","kind":"member","provider":"qoder","model":"planner"}
	      ]},
	      {"id":"def","label":"Default","fallback":true,"steps":[
	        {"id":"d","kind":"member","provider":"trae","model":"general"}
	      ]}
	    ]}
	  ]
	}`
	agg, _, path, err := CompilePipeline(Aggregation{ID: "mine", Document: doc}, nil, "hello", map[string]string{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if path != "Default" || agg.Members[0].Model != "general" {
		t.Fatalf("path=%s members=%+v", path, agg.Members)
	}
	labels := map[string]string{"qoder||small": "plan"}
	agg, _, path, err = CompilePipeline(Aggregation{ID: "mine", Document: doc}, nil, "hello", labels, nil)
	if err != nil {
		t.Fatal(err)
	}
	if path != "Plan" || agg.Members[0].Model != "planner" {
		t.Fatalf("label path=%s members=%+v", path, agg.Members)
	}
}

func TestPipelineNestedGroupKeepsInnerOrder(t *testing.T) {
	inner := `{
	  "version": 1,
	  "steps": [
	    {"id":"z","kind":"member","provider":"qoder","model":"model-z"},
	    {"id":"a","kind":"member","provider":"qoder","model":"model-a"}
	  ]
	}`
	outer := `{
	  "version": 1,
	  "steps": [
	    {"id":"g","kind":"group","aggregation_id":"inner","weight":5},
	    {"id":"tail","kind":"member","provider":"trae","model":"tail"}
	  ]
	}`
	catalog := map[string]Aggregation{
		"inner": {ID: "inner", Enabled: true, Strategy: AggregationRoundRobin, Document: inner},
	}
	_, seats, _, err := CompilePipeline(Aggregation{ID: "outer", Document: outer, Strategy: AggregationPriority}, catalog, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(seats) != 2 || seats[0].Kind != "group" || seats[0].GroupStrategy != AggregationRoundRobin {
		t.Fatalf("seats = %+v", seats)
	}
	if len(seats[0].GroupMembers) != 2 || seats[0].GroupMembers[0].Model != "model-z" || seats[0].GroupMembers[1].Model != "model-a" {
		t.Fatalf("inner = %+v", seats[0].GroupMembers)
	}
	if seats[1].Member.Model != "tail" {
		t.Fatalf("tail = %+v", seats[1])
	}
}

func TestPipelineRejectsCycleDepthAndLogicalClassifier(t *testing.T) {
	cycle := `{"version":1,"steps":[{"id":"g","kind":"group","aggregation_id":"self"}]}`
	if _, _, err := CollectPipeline("self", cycle, map[string]Aggregation{"self": {ID: "self", Enabled: true}}); err == nil {
		t.Fatal("expected cycle")
	}
	deep := `{"version":1,"steps":[{"id":"g","kind":"group","aggregation_id":"a"}]}`
	catalog := map[string]Aggregation{
		"a": {ID: "a", Enabled: true, Document: `{"version":1,"steps":[{"id":"g","kind":"group","aggregation_id":"b"}]}`},
		"b": {ID: "b", Enabled: true, Document: `{"version":1,"steps":[{"id":"g","kind":"group","aggregation_id":"c"}]}`},
		"c": {ID: "c", Enabled: true, Document: `{"version":1,"steps":[{"id":"g","kind":"group","aggregation_id":"d"}]}`},
		"d": {ID: "d", Enabled: true, Document: `{"version":1,"steps":[{"id":"m","kind":"member","provider":"qoder","model":"m"}]}`},
	}
	if _, _, err := CollectPipeline("root", deep, catalog); err == nil {
		t.Fatal("expected depth limit")
	}
	bad := `{"version":1,"steps":[{"id":"b","kind":"branch","classifier":{"provider":"aggregation","model":"mine"},"paths":[{"id":"p","label":"P","steps":[{"id":"m","kind":"member","provider":"qoder","model":"m"}]}]}]}`
	if _, _, err := CollectPipeline("mine", bad, nil); err == nil {
		t.Fatal("expected concrete classifier")
	}
}
