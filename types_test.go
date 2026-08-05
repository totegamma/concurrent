package concrnt

import (
	"testing"
)

func boolPtr(b bool) *bool { return &b }

func TestEventPublicView(t *testing.T) {
	event := Event{
		Type: "created",
		URI:  "cckv://owner/timelines/home/items/x",
		References: map[string]SignedDocument{
			"cckv://owner/timelines/home/items/x": {
				Document: "item",
				IsPublic: boolPtr(true),
				References: map[string]SignedDocument{
					"cckv://owner/posts/public": {
						Document: "public",
						IsPublic: boolPtr(true),
						References: map[string]SignedDocument{
							"cckv://owner/posts/deep": {Document: "deep", IsPublic: boolPtr(true)},
						},
					},
					"cckv://owner/posts/protected": {Document: "protected", IsPublic: boolPtr(false)},
					"cckv://owner/posts/unmarked":  {Document: "unmarked"},
				},
			},
			"cckv://owner/posts/hidden":   {Document: "hidden", IsPublic: boolPtr(false)},
			"cckv://owner/posts/unmarked": {Document: "unmarked"},
		},
	}

	public := event.PublicView()

	if len(public.References) != 1 {
		t.Fatalf("only the public document must survive, got %v", public.References)
	}
	item, ok := public.References["cckv://owner/timelines/home/items/x"]
	if !ok {
		t.Fatalf("public document must survive, got %v", public.References)
	}
	if item.IsPublic != nil {
		t.Fatal("the internal flag must be stripped from the public view")
	}
	if len(item.References) != 1 {
		t.Fatalf("only the public nested reference must survive, got %v", item.References)
	}
	nested, ok := item.References["cckv://owner/posts/public"]
	if !ok {
		t.Fatalf("public nested reference must survive, got %v", item.References)
	}
	if nested.IsPublic != nil {
		t.Fatal("the internal flag must be stripped from nested references")
	}
	if nested.References != nil {
		t.Fatalf("depth-2 references must be removed outright, got %v", nested.References)
	}

	// the receiver must stay intact — internal consumers share it
	if event.References["cckv://owner/posts/hidden"].Document != "hidden" {
		t.Fatal("PublicView must not mutate the receiver")
	}
	if event.References["cckv://owner/timelines/home/items/x"].IsPublic == nil {
		t.Fatal("PublicView must not strip flags on the receiver")
	}
	if len(event.References["cckv://owner/timelines/home/items/x"].References) != 3 {
		t.Fatal("PublicView must not filter the receiver's nested references")
	}
}

func TestEventMarkAllPublic(t *testing.T) {
	event := Event{
		Type: "created",
		References: map[string]SignedDocument{
			"cckv://owner/posts/a": {
				Document: "a",
				References: map[string]SignedDocument{
					"cckv://owner/posts/b": {Document: "b"},
				},
			},
		},
	}

	marked := event.MarkAllPublic()

	top := marked.References["cckv://owner/posts/a"]
	if top.IsPublic == nil || !*top.IsPublic {
		t.Fatalf("top-level document must be flagged public, got %+v", top.IsPublic)
	}
	nested := top.References["cckv://owner/posts/b"]
	if nested.IsPublic == nil || !*nested.IsPublic {
		t.Fatalf("nested document must be flagged public, got %+v", nested.IsPublic)
	}

	if event.References["cckv://owner/posts/a"].IsPublic != nil {
		t.Fatal("MarkAllPublic must not mutate the receiver")
	}
	if event.References["cckv://owner/posts/a"].References["cckv://owner/posts/b"].IsPublic != nil {
		t.Fatal("MarkAllPublic must not mutate the receiver's nested references")
	}
}

func TestSignedDocumentStripInternalFlags(t *testing.T) {
	sd := SignedDocument{
		Document: "top",
		IsPublic: boolPtr(true),
		References: map[string]SignedDocument{
			"cckv://owner/posts/a": {
				Document: "a",
				IsPublic: boolPtr(false),
				References: map[string]SignedDocument{
					"cckv://owner/posts/b": {Document: "b", IsPublic: boolPtr(true)},
				},
			},
		},
	}

	stripped := sd.StripInternalFlags()

	if stripped.IsPublic != nil {
		t.Fatal("top-level flag must be stripped")
	}
	ref := stripped.References["cckv://owner/posts/a"]
	if ref.IsPublic != nil {
		t.Fatal("nested flag must be stripped")
	}
	if ref.References["cckv://owner/posts/b"].IsPublic != nil {
		t.Fatal("deeply nested flag must be stripped")
	}
	if ref.References["cckv://owner/posts/b"].Document != "b" {
		t.Fatal("documents must be preserved")
	}

	if sd.IsPublic == nil {
		t.Fatal("StripInternalFlags must not mutate the receiver")
	}
	if sd.References["cckv://owner/posts/a"].IsPublic == nil {
		t.Fatal("StripInternalFlags must not mutate the receiver's references")
	}
}
