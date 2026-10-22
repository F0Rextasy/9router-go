package db

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

// The dashboard's provider-node list reads every node's lifetime token spend.
// That read is the one that grew with the ledger rather than with the number
// of nodes, because it aggregated every usageHistory row by provider — rows
// belonging to providers that are not custom nodes included.
//
// Two properties are pinned here, and both are properties of the plan rather
// than of a timing:
//
//   - the spend of a node is read by an index seek that covers the token
//     columns, so the statement does not visit the table once per matching
//     row; and
//   - the statement walks providerNodes, so the cost scales with the number of
//     nodes and not with the size of the ledger.
//
// A timing assertion would not survive a different machine, and it would not
// say what regressed. The plan does.

// TestProviderTokenUsageByNode_SeeksTheProviderIndex proves the per-node
// spend is read through the composite provider index rather than by scanning
// usageHistory.
func TestProviderTokenUsageByNode_SeeksTheProviderIndex(t *testing.T) {
	repo := openEphemeralSchemaDB(t)
	database := repo.RawDB()

	plan := explainQueryPlan(t, database, providerTokenUsageByNodeQuery)

	// The spend is read under the alias "h". A plan row that searches it
	// without saying COVERING means the sum still went back to the table once
	// per matching row — which is the cost the composite index removes.
	var seekDetail string
	for _, detail := range plan {
		if strings.Contains(detail, "SEARCH h") {
			seekDetail = detail
		}
	}
	if seekDetail == "" {
		t.Fatalf("plan never searches the ledger:\n%s", strings.Join(plan, "\n"))
	}
	if !strings.Contains(seekDetail, "COVERING INDEX") {
		t.Errorf("the node spend is not a covering-index read: %q", seekDetail)
	}
	if !strings.Contains(seekDetail, "idx_uh_prov_tok") {
		t.Errorf("the node spend is not read through idx_uh_prov_tok: %q", seekDetail)
	}
}

// driven by providerNodes, not by the ledger. A scan of usageHistory here is
// the regression: it is what made the node list cost a second on a 400k-row
// ledger and grow with every request the gateway served.
func TestProviderTokenUsageByNode_DoesNotScanTheLedger(t *testing.T) {
	repo := openEphemeralSchemaDB(t)
	database := repo.RawDB()

	plan := explainQueryPlan(t, database, providerTokenUsageByNodeQuery)

	for _, detail := range plan {
		if strings.Contains(detail, "SCAN usageHistory") {
			t.Errorf("the node list scans the whole ledger: %q", detail)
		}
	}
}

// TestProviderTokenUsageByNode_ReadsOnlyTheNodesLedger proves the answer is
// the same one the per-node reader gives — the sum since the node's own reset
// mark, plus whatever a prune carried over.
func TestProviderTokenUsageByNode_ReadsOnlyTheNodesLedger(t *testing.T) {
	repo := openEphemeralSchemaDB(t)
	ctx := context.Background()

	seedProviderNode(t, repo, "node-nara", "Nara AI")
	seedProviderNode(t, repo, "node-bare", "Bare AI")
	// A provider that is not a custom node at all: its spend belongs to no
	// node, and the list must not report it as one.
	seedProviderLedgerRow(t, repo, 0, "openai", 100, 50, time.Now())

	seedProviderLedgerRow(t, repo, 0, "node-nara", 100, 50, time.Now())
	seedProviderLedgerRow(t, repo, 0, "node-nara", 200, 100, time.Now())

	// The node's own reset mark forgives everything before it.
	if _, err := repo.RawDB().Exec(
		`UPDATE providerNodes SET quotaResetId = (SELECT MAX(id) FROM usageHistory WHERE provider = 'node-nara')
		  WHERE id = 'node-nara'`); err != nil {
		t.Fatalf("set reset mark: %v", err)
	}
	seedProviderLedgerRow(t, repo, 0, "node-nara", 7, 3, time.Now())

	usage, err := repo.ProviderTokenUsageByNode(ctx)
	if err != nil {
		t.Fatalf("ProviderTokenUsageByNode: %v", err)
	}

	if got := usage["node-nara"]; got != 10 {
		t.Errorf("node-nara spend = %d, want 10 (the rows after its reset mark)", got)
	}
	if got := usage["node-bare"]; got != 0 {
		t.Errorf("node-bare spend = %d, want 0", got)
	}
	if _, ok := usage["openai"]; ok {
		t.Errorf("a provider that is not a custom node was reported as one: %+v", usage)
	}
}

// TestProviderTokenUsageByNode_AddsTheCarryover proves a node whose history a
// prune already deleted still reports the tokens that were carried over. The
// carryover is added, not joined, so those rows are not dropped.
func TestProviderTokenUsageByNode_AddsTheCarryover(t *testing.T) {
	repo := openEphemeralSchemaDB(t)
	ctx := context.Background()

	seedProviderNode(t, repo, "node-nara", "Nara AI")
	if _, err := repo.RawDB().Exec(
		`INSERT INTO providerLedgerCarryover (providerId, tokens, updatedAt) VALUES ('node-nara', 900, ?)`,
		time.Now()); err != nil {
		t.Fatalf("seed carryover: %v", err)
	}

	usage, err := repo.ProviderTokenUsageByNode(ctx)
	if err != nil {
		t.Fatalf("ProviderTokenUsageByNode: %v", err)
	}
	if got := usage["node-nara"]; got != 900 {
		t.Errorf("node-nara spend = %d, want 900 from the carryover", got)
	}
}

// TestProviderTokenUsageByNode_ReportsEveryNode proves a node with no history
// and no carryover is still reported, with zero. The list renders a node's
// spend next to its quota, and a node missing from the map is a node the
// dashboard cannot show a zero for.
func TestProviderTokenUsageByNode_ReportsEveryNode(t *testing.T) {
	repo := openEphemeralSchemaDB(t)
	ctx := context.Background()

	seedProviderNode(t, repo, "node-nara", "Nara AI")
	seedProviderNode(t, repo, "node-bare", "Bare AI")

	usage, err := repo.ProviderTokenUsageByNode(ctx)
	if err != nil {
		t.Fatalf("ProviderTokenUsageByNode: %v", err)
	}
	if len(usage) != 2 {
		t.Fatalf("reported %d nodes, want 2: %+v", len(usage), usage)
	}
	for _, id := range []string{"node-nara", "node-bare"} {
		if _, ok := usage[id]; !ok {
			t.Errorf("node %s is absent from the report", id)
		}
	}
}

// TestUsageHistoryProviderIndexCoversTheTokenSum proves the composite index
// carries the token columns, so a node's spend is a covering-index read. An
// index on (provider) alone answers the equality search but still sends the
// sum back to the table once per matching row.
func TestUsageHistoryProviderIndexCoversTheTokenSum(t *testing.T) {
	repo := openEphemeralSchemaDB(t)
	database := repo.RawDB()

	var columns string
	if err := database.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = 'idx_uh_prov_tok'`,
	).Scan(&columns); err != nil {
		t.Fatalf("read idx_uh_prov_tok: %v", err)
	}
	for _, column := range []string{"provider", "id", "promptTokens", "completionTokens"} {
		if !strings.Contains(columns, column) {
			t.Errorf("idx_uh_prov_tok does not carry %s: %q", column, columns)
		}
	}

	// The narrow index it replaced is gone: SQLite would otherwise keep it as
	// a second candidate every ledger insert has to maintain.
	var narrow int
	if err := database.QueryRow(
		`SELECT COUNT(1) FROM sqlite_master WHERE type = 'index' AND name = 'idx_uh_provider'`,
	).Scan(&narrow); err != nil {
		t.Fatalf("check idx_uh_provider: %v", err)
	}
	if narrow != 0 {
		t.Error("idx_uh_provider is still present; it is superseded by idx_uh_prov_tok")
	}
}

// TestUsageHistoryProviderIndexAddedOnUpgrade proves a database that predates
// the composite index picks it up on the next boot, and that the narrow index
// it replaces is dropped.
func TestUsageHistoryProviderIndexAddedOnUpgrade(t *testing.T) {
	repo := openEphemeralSchemaDB(t)
	database := repo.RawDB()

	// Recreate the pre-fix shape.
	if _, err := database.Exec(`DROP INDEX IF EXISTS idx_uh_prov_tok`); err != nil {
		t.Fatalf("drop composite index: %v", err)
	}
	if _, err := database.Exec(`CREATE INDEX idx_uh_provider ON usageHistory(provider)`); err != nil {
		t.Fatalf("recreate narrow index: %v", err)
	}

	if err := EnsureCoreSchema(database); err != nil {
		t.Fatalf("EnsureCoreSchema: %v", err)
	}

	var composite, narrow int
	if err := database.QueryRow(
		`SELECT COUNT(1) FROM sqlite_master WHERE type = 'index' AND name = 'idx_uh_prov_tok'`,
	).Scan(&composite); err != nil {
		t.Fatalf("check composite index: %v", err)
	}
	if composite != 1 {
		t.Error("idx_uh_prov_tok was not created on upgrade")
	}
	if err := database.QueryRow(
		`SELECT COUNT(1) FROM sqlite_master WHERE type = 'index' AND name = 'idx_uh_provider'`,
	).Scan(&narrow); err != nil {
		t.Fatalf("check narrow index: %v", err)
	}
	if narrow != 0 {
		t.Error("idx_uh_provider was not dropped on upgrade")
	}
}

// TestProviderTokenUsageByNode_PrefersTheCoveringIndex proves the read still
// picks the composite index when the narrow one is also present.
//
// Upstream's schema.js still declares idx_uh_provider, so the Next.js
// dashboard booted on the same data.sqlite recreates it. Both indexes then
// exist, and the read must not fall back to the narrow one — that is the
// difference between a covering read and a per-row table visit.
func TestProviderTokenUsageByNode_PrefersTheCoveringIndex(t *testing.T) {
	repo := openEphemeralSchemaDB(t)
	database := repo.RawDB()

	if _, err := database.Exec(`CREATE INDEX IF NOT EXISTS idx_uh_provider ON usageHistory(provider)`); err != nil {
		t.Fatalf("recreate narrow index: %v", err)
	}

	plan := explainQueryPlan(t, database, providerTokenUsageByNodeQuery)

	var seekDetail string
	for _, detail := range plan {
		if strings.Contains(detail, "SEARCH h") {
			seekDetail = detail
		}
	}
	if seekDetail == "" {
		t.Fatalf("plan never searches the ledger:\n%s", strings.Join(plan, "\n"))
	}
	if !strings.Contains(seekDetail, "COVERING INDEX") || !strings.Contains(seekDetail, "idx_uh_prov_tok") {
		t.Errorf("the node spend did not use the covering index: %q", seekDetail)
	}
}

// TestProviderTokenUsageByNode_ScalesWithNodesNotLedger proves the list does
// not read the ledger's other providers. A ledger full of rows for providers
// that are not custom nodes must not change what the node list costs, so the
// test compares the plan the statement runs over a populated ledger with the
// plan it runs over an empty one.
func TestProviderTokenUsageByNode_ScalesWithNodesNotLedger(t *testing.T) {
	repo := openEphemeralSchemaDB(t)
	database := repo.RawDB()

	seedProviderNode(t, repo, "node-nara", "Nara AI")
	empty := explainQueryPlan(t, database, providerTokenUsageByNodeQuery)

	for i := range 200 {
		seedProviderLedgerRow(t, repo, 0, "openai", 100, 50, time.Now())
		_ = i
	}
	populated := explainQueryPlan(t, database, providerTokenUsageByNodeQuery)

	if len(empty) != len(populated) {
		t.Errorf("the plan changed once the ledger filled: %d rows before, %d after\nbefore:\n%s\nafter:\n%s",
			len(empty), len(populated), strings.Join(empty, "\n"), strings.Join(populated, "\n"))
	}
}

var _ = sql.ErrNoRows
