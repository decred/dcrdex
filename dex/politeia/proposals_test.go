// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.
package pi

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"testing"

	"github.com/decred/politeia/politeiad/plugins/usermd"
	piv1 "github.com/decred/politeia/politeiawww/api/pi/v1"
	rv1 "github.com/decred/politeia/politeiawww/api/records/v1"
	tv1 "github.com/decred/politeia/politeiawww/api/ticketvote/v1"
)

func TestPaginateTokens(t *testing.T) {
	tests := []struct {
		name     string
		tokens   []string
		pageSize uint32
		want     [][]string
	}{
		{
			name:     "2 tokens / 5 page size",
			tokens:   []string{"x", "y"},
			pageSize: 5,
			want: [][]string{
				{"x", "y"},
			},
		},
		{
			name:     "6 tokens / 5 page size",
			tokens:   []string{"x", "y", "z", "v", "q", "w"},
			pageSize: 5,
			want: [][]string{
				{"x", "y", "z", "v", "q"},
				{"w"},
			},
		},
		{
			name: "21 tokens / 5 page size",
			tokens: []string{
				"x", "y", "z", "v", "q", "w", "x", "y", "z", "v", "q", "w",
				"x", "y", "z", "v", "q", "w", "q", "w", "e",
			},
			pageSize: 5,
			want: [][]string{
				{"x", "y", "z", "v", "q"},
				{"w", "x", "y", "z", "v"},
				{"q", "w", "x", "y", "z"},
				{"v", "q", "w", "q", "w"},
				{"e"},
			},
		},
		{
			name:     "2 tokens / 4 page size",
			tokens:   []string{"x", "y"},
			pageSize: 4,
			want: [][]string{
				{"x", "y"},
			},
		},
		{
			name:     "5 tokens / 4 page size",
			tokens:   []string{"x", "y", "z", "v", "q"},
			pageSize: 4,
			want: [][]string{
				{"x", "y", "z", "v"},
				{"q"},
			},
		},
		{
			name:     "9 tokens / 4 page size",
			tokens:   []string{"x", "y", "z", "v", "q", "x", "y", "z", "v"},
			pageSize: 4,
			want: [][]string{
				{"x", "y", "z", "v"},
				{"q", "x", "y", "z"},
				{"v"},
			},
		},
		{
			name:     "2 tokens / 3 page size",
			tokens:   []string{"x", "y"},
			pageSize: 3,
			want: [][]string{
				{"x", "y"},
			},
		},
		{
			name:     "7 tokens / 3 page size",
			tokens:   []string{"x", "y", "z", "v", "q", "w", "e"},
			pageSize: 3,
			want: [][]string{
				{"x", "y", "z"},
				{"v", "q", "w"},
				{"e"},
			},
		},
		{
			name:     "0 tokens / 2 page size",
			tokens:   []string{},
			pageSize: 2,
			want:     [][]string{},
		},
		{
			name:     "1 tokens / 2 page size",
			tokens:   []string{"x"},
			pageSize: 2,
			want: [][]string{
				{"x"},
			},
		},
		{
			name:     "10 tokens / 2 page size",
			tokens:   []string{"x", "y", "z", "v", "q", "w", "x", "y", "z", "v"},
			pageSize: 2,
			want: [][]string{
				{"x", "y"}, {"z", "v"}, {"q", "w"}, {"x", "y"}, {"z", "v"},
			},
		},
		{
			name:     "0 tokens / 1 page size",
			tokens:   []string{},
			pageSize: 1,
			want:     [][]string{},
		},
		{
			name:     "1 tokens / 1 page size",
			tokens:   []string{"x"},
			pageSize: 1,
			want: [][]string{
				{"x"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := paginateTokens(tt.tokens, tt.pageSize); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("paginateTokens() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProposalKey(t *testing.T) {
	tests := []struct {
		token string
		want  string
	}{
		{"abc123", "proposal:abc123"},
		{"", "proposal:"},
		{"tokenwithspecialchars!@#", "proposal:tokenwithspecialchars!@#"},
	}
	for _, tt := range tests {
		t.Run(tt.token, func(t *testing.T) {
			got := string(proposalKey(tt.token))
			if got != tt.want {
				t.Errorf("proposalKey(%q) = %q, want %q", tt.token, got, tt.want)
			}
		})
	}
}

func TestProposalsStatusIndex(t *testing.T) {
	p := &Proposal{
		Token:      "abc123",
		VoteStatus: tv1.VoteStatusStarted,
		Timestamp:  1000,
	}
	got := string(proposalsStatusIndex(p))
	reversed := uint64(math.MaxInt64) - 1000
	want := fmt.Sprintf("proposal:status:%s:%020d:%s", "Started", reversed, "abc123")
	if got != want {
		t.Errorf("proposalsStatusIndex() = %q, want %q", got, want)
	}
}

func TestProposalsStatusIndex_LargeTimestamp(t *testing.T) {
	// This tests the uint64 cast fix: using int64 subtraction with a timestamp
	// near MaxInt64 would overflow. Using uint64 prevents this.
	p := &Proposal{
		Token:      "token1",
		VoteStatus: tv1.VoteStatusApproved,
		Timestamp:  uint64(math.MaxInt64) - 1,
	}
	got := string(proposalsStatusIndex(p))
	reversed := uint64(math.MaxInt64) - p.Timestamp
	want := fmt.Sprintf("proposal:status:%s:%020d:%s", "Approved", reversed, "token1")
	if got != want {
		t.Errorf("proposalsStatusIndex() = %q, want %q", got, want)
	}
}

func TestProposalsTimestampIndex(t *testing.T) {
	p := &Proposal{
		Token:     "xyz789",
		Timestamp: 5000,
	}
	got := string(proposalsTimestampIndex(p))
	reversed := uint64(math.MaxInt64) - 5000
	want := fmt.Sprintf("proposal:ts:%020d:%s", reversed, "xyz789")
	if got != want {
		t.Errorf("proposalsTimestampIndex() = %q, want %q", got, want)
	}
}

func TestProposalsTimestampIndex_LargeTimestamp(t *testing.T) {
	p := &Proposal{
		Token:     "token2",
		Timestamp: uint64(math.MaxInt64) - 1,
	}
	got := string(proposalsTimestampIndex(p))
	reversed := uint64(math.MaxInt64) - p.Timestamp
	want := fmt.Sprintf("proposal:ts:%020d:%s", reversed, "token2")
	if got != want {
		t.Errorf("proposalsTimestampIndex() = %q, want %q", got, want)
	}
}

func TestProposalMarshalUnmarshalBinary(t *testing.T) {
	original := &Proposal{
		ID:               1,
		State:            rv1.RecordStateVetted,
		Status:           rv1.RecordStatusPublic,
		Token:            "abc123",
		Version:          2,
		Timestamp:        1234567890,
		Username:         "testuser",
		Description:      "Test proposal description",
		Name:             "Test Proposal",
		UserID:           "user123",
		CommentsCount:    5,
		VoteStatus:       tv1.VoteStatusStarted,
		EligibleTickets:  40960,
		StartBlockHeight: 100000,
		EndBlockHeight:   110000,
		QuorumPercentage: 20,
		PassPercentage:   60,
		TotalVotes:       1000,
		VoteResults: []tv1.VoteResult{
			{ID: "yes", Votes: 700},
			{ID: "no", Votes: 300},
		},
		Synced:      true,
		PublishedAt: 1234567800,
		CensoredAt:  0,
		AbandonedAt: 0,
	}

	data, err := original.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary error: %v", err)
	}

	decoded := new(Proposal)
	err = decoded.UnmarshalBinary(data)
	if err != nil {
		t.Fatalf("UnmarshalBinary error: %v", err)
	}

	if !original.IsEqual(*decoded) {
		t.Error("decoded proposal is not equal to original")
	}
	if decoded.Synced != original.Synced {
		t.Errorf("Synced: got %v, want %v", decoded.Synced, original.Synced)
	}
}

func TestIsEqual(t *testing.T) {
	base := func() Proposal {
		return Proposal{
			Token:            "abc123",
			Name:             "Test Proposal",
			State:            rv1.RecordStateVetted,
			Status:           rv1.RecordStatusPublic,
			StatusChangeMsg:  "approved",
			CommentsCount:    5,
			Timestamp:        1000,
			VoteStatus:       tv1.VoteStatusStarted,
			TotalVotes:       100,
			PublishedAt:      900,
			CensoredAt:       0,
			AbandonedAt:      0,
			Description:      "A test proposal",
			Version:          2,
			Username:         "testuser",
			EligibleTickets:  40960,
			StartBlockHeight: 100000,
			EndBlockHeight:   110000,
			QuorumPercentage: 20,
			PassPercentage:   60,
			VoteResults: []tv1.VoteResult{
				{ID: "yes", Votes: 70},
				{ID: "no", Votes: 30},
			},
		}
	}

	t.Run("identical proposals", func(t *testing.T) {
		a := base()
		b := base()
		if !a.IsEqual(b) {
			t.Error("identical proposals should be equal")
		}
	})

	// Test each field that should cause inequality.
	fieldTests := []struct {
		name   string
		modify func(*Proposal)
	}{
		{"Token", func(p *Proposal) { p.Token = "different" }},
		{"Name", func(p *Proposal) { p.Name = "Different Name" }},
		{"State", func(p *Proposal) { p.State = rv1.RecordStateUnvetted }},
		{"Status", func(p *Proposal) { p.Status = rv1.RecordStatusCensored }},
		{"StatusChangeMsg", func(p *Proposal) { p.StatusChangeMsg = "censored" }},
		{"CommentsCount", func(p *Proposal) { p.CommentsCount = 99 }},
		{"Timestamp", func(p *Proposal) { p.Timestamp = 2000 }},
		{"VoteStatus", func(p *Proposal) { p.VoteStatus = tv1.VoteStatusFinished }},
		{"TotalVotes", func(p *Proposal) { p.TotalVotes = 999 }},
		{"PublishedAt", func(p *Proposal) { p.PublishedAt = 800 }},
		{"CensoredAt", func(p *Proposal) { p.CensoredAt = 1100 }},
		{"AbandonedAt", func(p *Proposal) { p.AbandonedAt = 1200 }},
		{"Description", func(p *Proposal) { p.Description = "Different description" }},
		{"Version", func(p *Proposal) { p.Version = 3 }},
		{"Username", func(p *Proposal) { p.Username = "otheruser" }},
		{"EligibleTickets", func(p *Proposal) { p.EligibleTickets = 99999 }},
		{"StartBlockHeight", func(p *Proposal) { p.StartBlockHeight = 200000 }},
		{"EndBlockHeight", func(p *Proposal) { p.EndBlockHeight = 300000 }},
		{"QuorumPercentage", func(p *Proposal) { p.QuorumPercentage = 30 }},
		{"PassPercentage", func(p *Proposal) { p.PassPercentage = 70 }},
		{"VoteResults length", func(p *Proposal) {
			p.VoteResults = []tv1.VoteResult{{ID: "yes", Votes: 70}}
		}},
		{"VoteResults ID", func(p *Proposal) {
			p.VoteResults[0].ID = "abstain"
		}},
		{"VoteResults Votes", func(p *Proposal) {
			p.VoteResults[1].Votes = 999
		}},
	}

	for _, tt := range fieldTests {
		t.Run("different "+tt.name, func(t *testing.T) {
			a := base()
			b := base()
			tt.modify(&b)
			if a.IsEqual(b) {
				t.Errorf("proposals with different %s should not be equal", tt.name)
			}
		})
	}

	t.Run("both nil VoteResults", func(t *testing.T) {
		a := base()
		b := base()
		a.VoteResults = nil
		b.VoteResults = nil
		if !a.IsEqual(b) {
			t.Error("proposals with nil VoteResults should be equal")
		}
	})

	t.Run("one nil VoteResults", func(t *testing.T) {
		a := base()
		b := base()
		b.VoteResults = nil
		if a.IsEqual(b) {
			t.Error("proposal with VoteResults should not equal one without")
		}
	})

	t.Run("empty vs nil VoteResults", func(t *testing.T) {
		a := base()
		b := base()
		a.VoteResults = nil
		b.VoteResults = []tv1.VoteResult{}
		// Both have length 0, so IsEqual treats them the same.
		if !a.IsEqual(b) {
			t.Error("nil and empty VoteResults should be equal (both length 0)")
		}
	})

	t.Run("ignores ID field", func(t *testing.T) {
		a := base()
		b := base()
		a.ID = 1
		b.ID = 2
		if !a.IsEqual(b) {
			t.Error("ID field should not affect equality")
		}
	})

	t.Run("ignores Synced field", func(t *testing.T) {
		a := base()
		b := base()
		a.Synced = true
		b.Synced = false
		if !a.IsEqual(b) {
			t.Error("Synced field should not affect equality")
		}
	})
}

func TestMetadata(t *testing.T) {
	t.Run("started with votes", func(t *testing.T) {
		p := &Proposal{
			VoteStatus: tv1.VoteStatusStarted,
			State:      rv1.RecordStateVetted,
			Status:     rv1.RecordStatusPublic,
			VoteResults: []tv1.VoteResult{
				{ID: "yes", Votes: 7000},
				{ID: "no", Votes: 3000},
			},
			EligibleTickets:  40960,
			QuorumPercentage: 20,
			PassPercentage:   60,
		}
		meta := p.Metadata()
		if meta.Yes != 7000 {
			t.Errorf("Yes: got %d, want 7000", meta.Yes)
		}
		if meta.No != 3000 {
			t.Errorf("No: got %d, want 3000", meta.No)
		}
		if meta.VoteCount != 10000 {
			t.Errorf("VoteCount: got %d, want 10000", meta.VoteCount)
		}
		// quorum = 20% of 40960 = 8192
		if meta.QuorumCount != 8192 {
			t.Errorf("QuorumCount: got %d, want 8192", meta.QuorumCount)
		}
		// 10000/40960 = 24.4%, which is >= 20%
		if !meta.QuorumAchieved {
			t.Error("QuorumAchieved: got false, want true")
		}
		if meta.PassPercent != 60 {
			t.Errorf("PassPercent: got %f, want 60", meta.PassPercent)
		}
		// approval = 7000/10000 * 100 = 70%
		if meta.Approval != 70 {
			t.Errorf("Approval: got %f, want 70", meta.Approval)
		}
		// rejection = 3000/10000 * 100 = 30%
		if meta.Rejection != 30 {
			t.Errorf("Rejection: got %f, want 30", meta.Rejection)
		}
		if !meta.IsPassing {
			t.Error("IsPassing: got false, want true (70% > 60%)")
		}
		if meta.VoteStatusDesc != "Started" {
			t.Errorf("VoteStatusDesc: got %q, want %q", meta.VoteStatusDesc, "Started")
		}
	})

	t.Run("zero eligible tickets (division by zero guard)", func(t *testing.T) {
		p := &Proposal{
			VoteStatus: tv1.VoteStatusStarted,
			VoteResults: []tv1.VoteResult{
				{ID: "yes", Votes: 0},
				{ID: "no", Votes: 0},
			},
			EligibleTickets:  0,
			QuorumPercentage: 20,
			PassPercentage:   60,
		}
		meta := p.Metadata()
		if meta.QuorumAchieved {
			t.Error("QuorumAchieved should be false when EligibleTickets is 0")
		}
		if meta.QuorumCount != 0 {
			t.Errorf("QuorumCount: got %d, want 0", meta.QuorumCount)
		}
	})

	t.Run("not passing when approval below threshold", func(t *testing.T) {
		p := &Proposal{
			VoteStatus: tv1.VoteStatusStarted,
			VoteResults: []tv1.VoteResult{
				{ID: "yes", Votes: 4000},
				{ID: "no", Votes: 6000},
			},
			EligibleTickets:  40960,
			QuorumPercentage: 20,
			PassPercentage:   60,
		}
		meta := p.Metadata()
		if meta.IsPassing {
			t.Error("IsPassing: got true, want false (40% < 60%)")
		}
		if meta.Approval != 40 {
			t.Errorf("Approval: got %f, want 40", meta.Approval)
		}
	})

	t.Run("quorum not achieved", func(t *testing.T) {
		p := &Proposal{
			VoteStatus: tv1.VoteStatusStarted,
			VoteResults: []tv1.VoteResult{
				{ID: "yes", Votes: 100},
				{ID: "no", Votes: 50},
			},
			EligibleTickets:  40960,
			QuorumPercentage: 20,
			PassPercentage:   60,
		}
		meta := p.Metadata()
		// 150/40960 = 0.37%, much less than 20%
		if meta.QuorumAchieved {
			t.Error("QuorumAchieved: got true, want false")
		}
		// Denominator = max(150, 8192) = 8192
		// Approval = 100/8192 * 100 ≈ 1.22%
		if meta.Approval > 2 {
			t.Errorf("Approval should be low when below quorum, got %f", meta.Approval)
		}
	})

	t.Run("approved status", func(t *testing.T) {
		p := &Proposal{
			VoteStatus: tv1.VoteStatusApproved,
			VoteResults: []tv1.VoteResult{
				{ID: "yes", Votes: 8000},
				{ID: "no", Votes: 2000},
			},
			EligibleTickets:  40960,
			QuorumPercentage: 20,
			PassPercentage:   60,
		}
		meta := p.Metadata()
		if meta.VoteStatusDesc != "Approved" {
			t.Errorf("VoteStatusDesc: got %q, want %q", meta.VoteStatusDesc, "Approved")
		}
		if !meta.IsPassing {
			t.Error("IsPassing should be true for approved proposal")
		}
	})

	t.Run("rejected status", func(t *testing.T) {
		p := &Proposal{
			VoteStatus: tv1.VoteStatusRejected,
			VoteResults: []tv1.VoteResult{
				{ID: "yes", Votes: 2000},
				{ID: "no", Votes: 8000},
			},
			EligibleTickets:  40960,
			QuorumPercentage: 20,
			PassPercentage:   60,
		}
		meta := p.Metadata()
		if meta.VoteStatusDesc != "Rejected" {
			t.Errorf("VoteStatusDesc: got %q, want %q", meta.VoteStatusDesc, "Rejected")
		}
		if meta.IsPassing {
			t.Error("IsPassing should be false for rejected proposal")
		}
	})

	t.Run("unauthorized status (no vote data)", func(t *testing.T) {
		p := &Proposal{
			VoteStatus: tv1.VoteStatusUnauthorized,
			State:      rv1.RecordStateVetted,
			Status:     rv1.RecordStatusPublic,
		}
		meta := p.Metadata()
		if meta.Yes != 0 || meta.No != 0 || meta.VoteCount != 0 {
			t.Error("unauthorized proposal should have zero vote counts")
		}
		if meta.QuorumAchieved {
			t.Error("QuorumAchieved should be false for unauthorized proposal")
		}
		if meta.IsPassing {
			t.Error("IsPassing should be false for unauthorized proposal")
		}
		if meta.VoteStatusDesc != "Unauthorized" {
			t.Errorf("VoteStatusDesc: got %q, want %q", meta.VoteStatusDesc, "Unauthorized")
		}
	})

	t.Run("zero votes on started proposal", func(t *testing.T) {
		p := &Proposal{
			VoteStatus:       tv1.VoteStatusStarted,
			VoteResults:      []tv1.VoteResult{},
			EligibleTickets:  40960,
			QuorumPercentage: 20,
			PassPercentage:   60,
		}
		meta := p.Metadata()
		if meta.VoteCount != 0 {
			t.Errorf("VoteCount: got %d, want 0", meta.VoteCount)
		}
		if meta.Approval != 0 {
			t.Errorf("Approval should be 0 with no votes, got %f", meta.Approval)
		}
		if meta.IsPassing {
			t.Error("IsPassing should be false with no votes")
		}
	})
}

func TestToMiniProposal(t *testing.T) {
	p := &Proposal{
		Token:      "abc123",
		Name:       "Test Proposal",
		Username:   "testuser",
		Version:    2,
		VoteStatus: tv1.VoteStatusStarted,
	}
	mini := p.ToMiniProposal()
	if mini.Token != "abc123" {
		t.Errorf("Token: got %q, want %q", mini.Token, "abc123")
	}
	if mini.Name != "Test Proposal" {
		t.Errorf("Name: got %q, want %q", mini.Name, "Test Proposal")
	}
	if mini.Username != "testuser" {
		t.Errorf("Username: got %q, want %q", mini.Username, "testuser")
	}
	if mini.Version != 2 {
		t.Errorf("Version: got %d, want 2", mini.Version)
	}
	if mini.VoteStatus != "Started" {
		t.Errorf("VoteStatus: got %q, want %q", mini.VoteStatus, "Started")
	}
}

func TestUserMetadataDecode(t *testing.T) {
	t.Run("valid user metadata", func(t *testing.T) {
		um := usermd.UserMetadata{
			UserID:    "user123",
			PublicKey: "pubkey456",
			Signature: "sig789",
		}
		payload, err := json.Marshal(um)
		if err != nil {
			t.Fatal(err)
		}
		ms := []rv1.MetadataStream{
			{
				PluginID: usermd.PluginID,
				StreamID: usermd.StreamIDUserMetadata,
				Payload:  string(payload),
			},
		}
		got, err := userMetadataDecode(ms)
		if err != nil {
			t.Fatalf("userMetadataDecode error: %v", err)
		}
		if got.UserID != "user123" {
			t.Errorf("UserID: got %q, want %q", got.UserID, "user123")
		}
		if got.PublicKey != "pubkey456" {
			t.Errorf("PublicKey: got %q, want %q", got.PublicKey, "pubkey456")
		}
	})

	t.Run("no user metadata present", func(t *testing.T) {
		ms := []rv1.MetadataStream{
			{
				PluginID: "otherplugin",
				StreamID: 99,
				Payload:  "{}",
			},
		}
		_, err := userMetadataDecode(ms)
		if err == nil {
			t.Fatal("expected error when no user metadata present")
		}
	})

	t.Run("empty metadata streams", func(t *testing.T) {
		_, err := userMetadataDecode(nil)
		if err == nil {
			t.Fatal("expected error for nil metadata streams")
		}
	})

	t.Run("invalid JSON payload", func(t *testing.T) {
		ms := []rv1.MetadataStream{
			{
				PluginID: usermd.PluginID,
				StreamID: usermd.StreamIDUserMetadata,
				Payload:  "invalid json",
			},
		}
		_, err := userMetadataDecode(ms)
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})

	t.Run("skips non-user metadata streams", func(t *testing.T) {
		um := usermd.UserMetadata{UserID: "correctuser"}
		payload, _ := json.Marshal(um)
		ms := []rv1.MetadataStream{
			{PluginID: "otherplugin", StreamID: 1, Payload: "{}"},
			{PluginID: usermd.PluginID, StreamID: 99, Payload: "{}"},
			{PluginID: usermd.PluginID, StreamID: usermd.StreamIDUserMetadata, Payload: string(payload)},
		}
		got, err := userMetadataDecode(ms)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.UserID != "correctuser" {
			t.Errorf("UserID: got %q, want %q", got.UserID, "correctuser")
		}
	})
}

func TestProposalMetadataDecode(t *testing.T) {
	makeFiles := func(name string, desc string) []rv1.File {
		pm := piv1.ProposalMetadata{Name: name}
		pmJSON, _ := json.Marshal(pm)
		return []rv1.File{
			{
				Name:    piv1.FileNameIndexFile,
				Payload: base64.StdEncoding.EncodeToString([]byte(desc)),
			},
			{
				Name:    piv1.FileNameProposalMetadata,
				Payload: base64.StdEncoding.EncodeToString(pmJSON),
			},
		}
	}

	t.Run("valid files", func(t *testing.T) {
		files := makeFiles("Test Proposal", "# Description\nThis is a test.")
		pm, desc, err := proposalMetadataDecode(files)
		if err != nil {
			t.Fatalf("proposalMetadataDecode error: %v", err)
		}
		if pm.Name != "Test Proposal" {
			t.Errorf("Name: got %q, want %q", pm.Name, "Test Proposal")
		}
		if desc != "# Description\nThis is a test." {
			t.Errorf("Description: got %q, want %q", desc, "# Description\nThis is a test.")
		}
	})

	t.Run("missing index file", func(t *testing.T) {
		pm := piv1.ProposalMetadata{Name: "Test"}
		pmJSON, _ := json.Marshal(pm)
		files := []rv1.File{
			{
				Name:    piv1.FileNameProposalMetadata,
				Payload: base64.StdEncoding.EncodeToString(pmJSON),
			},
		}
		_, _, err := proposalMetadataDecode(files)
		if err == nil {
			t.Fatal("expected error for missing index file")
		}
	})

	t.Run("missing proposal metadata file", func(t *testing.T) {
		files := []rv1.File{
			{
				Name:    piv1.FileNameIndexFile,
				Payload: base64.StdEncoding.EncodeToString([]byte("description")),
			},
		}
		_, _, err := proposalMetadataDecode(files)
		if err == nil {
			t.Fatal("expected error for missing proposal metadata file")
		}
	})

	t.Run("invalid base64 in index file", func(t *testing.T) {
		pm := piv1.ProposalMetadata{Name: "Test"}
		pmJSON, _ := json.Marshal(pm)
		files := []rv1.File{
			{Name: piv1.FileNameIndexFile, Payload: "!!!invalid-base64!!!"},
			{Name: piv1.FileNameProposalMetadata, Payload: base64.StdEncoding.EncodeToString(pmJSON)},
		}
		_, _, err := proposalMetadataDecode(files)
		if err == nil {
			t.Fatal("expected error for invalid base64")
		}
	})

	t.Run("invalid base64 in metadata file", func(t *testing.T) {
		files := []rv1.File{
			{Name: piv1.FileNameIndexFile, Payload: base64.StdEncoding.EncodeToString([]byte("desc"))},
			{Name: piv1.FileNameProposalMetadata, Payload: "!!!invalid-base64!!!"},
		}
		_, _, err := proposalMetadataDecode(files)
		if err == nil {
			t.Fatal("expected error for invalid base64")
		}
	})

	t.Run("invalid JSON in metadata file", func(t *testing.T) {
		files := []rv1.File{
			{Name: piv1.FileNameIndexFile, Payload: base64.StdEncoding.EncodeToString([]byte("desc"))},
			{Name: piv1.FileNameProposalMetadata, Payload: base64.StdEncoding.EncodeToString([]byte("not json"))},
		}
		_, _, err := proposalMetadataDecode(files)
		if err == nil {
			t.Fatal("expected error for invalid JSON in metadata")
		}
	})

	t.Run("empty files list", func(t *testing.T) {
		_, _, err := proposalMetadataDecode(nil)
		if err == nil {
			t.Fatal("expected error for nil files")
		}
	})

	t.Run("extra files are ignored", func(t *testing.T) {
		files := makeFiles("Test", "desc")
		files = append(files, rv1.File{Name: "extra.png", Payload: "data"})
		pm, desc, err := proposalMetadataDecode(files)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if pm.Name != "Test" || desc != "desc" {
			t.Error("extra files should not affect parsing")
		}
	})
}

func TestStatusChangeMetadataDecode(t *testing.T) {
	makeStatusPayload := func(statuses ...usermd.StatusChangeMetadata) string {
		var payload string
		for _, s := range statuses {
			b, _ := json.Marshal(s)
			payload += string(b)
		}
		return payload
	}

	t.Run("published status", func(t *testing.T) {
		md := []rv1.MetadataStream{
			{
				PluginID: usermd.PluginID,
				StreamID: usermd.StreamIDStatusChanges,
				Payload: makeStatusPayload(usermd.StatusChangeMetadata{
					Status:    uint32(rv1.RecordStatusPublic),
					Timestamp: 1000,
					Reason:    "looks good",
				}),
			},
		}
		ts, msg, err := statusChangeMetadataDecode(md)
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if ts.published != 1000 {
			t.Errorf("published: got %d, want 1000", ts.published)
		}
		if ts.censored != 0 || ts.abandoned != 0 {
			t.Error("censored and abandoned should be 0")
		}
		if msg != "looks good" {
			t.Errorf("changeMsg: got %q, want %q", msg, "looks good")
		}
	})

	t.Run("multiple status changes", func(t *testing.T) {
		md := []rv1.MetadataStream{
			{
				PluginID: usermd.PluginID,
				StreamID: usermd.StreamIDStatusChanges,
				Payload: makeStatusPayload(
					usermd.StatusChangeMetadata{
						Status:    uint32(rv1.RecordStatusPublic),
						Timestamp: 1000,
						Reason:    "published",
					},
					usermd.StatusChangeMetadata{
						Status:    uint32(rv1.RecordStatusArchived),
						Timestamp: 2000,
						Reason:    "abandoned after review",
					},
				),
			},
		}
		ts, msg, err := statusChangeMetadataDecode(md)
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if ts.published != 1000 {
			t.Errorf("published: got %d, want 1000", ts.published)
		}
		if ts.abandoned != 2000 {
			t.Errorf("abandoned: got %d, want 2000", ts.abandoned)
		}
		// Latest message should be from the most recent status change.
		if msg != "abandoned after review" {
			t.Errorf("changeMsg: got %q, want %q", msg, "abandoned after review")
		}
	})

	t.Run("censored status", func(t *testing.T) {
		md := []rv1.MetadataStream{
			{
				PluginID: usermd.PluginID,
				StreamID: usermd.StreamIDStatusChanges,
				Payload: makeStatusPayload(usermd.StatusChangeMetadata{
					Status:    uint32(rv1.RecordStatusCensored),
					Timestamp: 1500,
					Reason:    "spam",
				}),
			},
		}
		ts, _, err := statusChangeMetadataDecode(md)
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if ts.censored != 1500 {
			t.Errorf("censored: got %d, want 1500", ts.censored)
		}
	})

	t.Run("no status change metadata", func(t *testing.T) {
		md := []rv1.MetadataStream{
			{PluginID: "otherplugin", StreamID: 99, Payload: "{}"},
		}
		ts, msg, err := statusChangeMetadataDecode(md)
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if ts.published != 0 || ts.censored != 0 || ts.abandoned != 0 {
			t.Error("all timestamps should be 0 when no status change metadata")
		}
		if msg != "" {
			t.Errorf("changeMsg should be empty, got %q", msg)
		}
	})

	t.Run("empty metadata streams", func(t *testing.T) {
		ts, msg, err := statusChangeMetadataDecode(nil)
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if ts.published != 0 || ts.censored != 0 || ts.abandoned != 0 {
			t.Error("all timestamps should be 0 for nil metadata")
		}
		if msg != "" {
			t.Errorf("changeMsg should be empty, got %q", msg)
		}
	})

	t.Run("invalid JSON in payload", func(t *testing.T) {
		md := []rv1.MetadataStream{
			{
				PluginID: usermd.PluginID,
				StreamID: usermd.StreamIDStatusChanges,
				Payload:  "invalid json content here",
			},
		}
		_, _, err := statusChangeMetadataDecode(md)
		if err == nil {
			t.Fatal("expected error for invalid JSON payload")
		}
	})
}

func TestInProgressStatuses(t *testing.T) {
	expected := []string{"Unauthorized", "Authorized", "Started"}
	if !reflect.DeepEqual(inProgressStatuses, expected) {
		t.Errorf("inProgressStatuses = %v, want %v", inProgressStatuses, expected)
	}
}
