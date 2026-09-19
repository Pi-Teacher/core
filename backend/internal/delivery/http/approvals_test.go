package httpapi_test

import (
	"fmt"
	"net/http"
	"testing"
)

// approvalSummary 是 202 响应里的 approval 对象.
type approvalSummary struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
}

// approvalDetail 是审批详情响应体 (测试用子集).
type approvalDetail struct {
	ID              int64   `json:"id"`
	Operation       string  `json:"operation"`
	EntityType      string  `json:"entity_type"`
	Status          string  `json:"status"`
	OriginalPayload any     `json:"original_payload"`
	ApprovedPayload any     `json:"approved_payload"`
	Reason          *string `json:"reason"`
	Targets         []struct {
		EntityType  string `json:"entity_type"`
		EntityID    int64  `json:"entity_id"`
		BaseVersion int64  `json:"base_version"`
		Role        string `json:"role"`
	} `json:"targets"`
}

// approveCLI 提交一条 CLI 提案并返回审批 ID.
func approveCLI(t *testing.T, cli *cliClient, method, path string, body any, key string) int64 {
	t.Helper()
	resp := cli.do(method, path, body, key)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("cli propose %s %s status = %d, want 202", method, path, resp.StatusCode)
	}
	var out struct {
		Approval approvalSummary `json:"approval"`
	}
	decodeBody(t, resp, &out)
	if out.Approval.Status != "pending" {
		t.Fatalf("proposal status = %s, want pending", out.Approval.Status)
	}
	return out.Approval.ID
}

// TestThirdBatchAcceptance 覆盖第三批验收: CLI 提交提案 → Web 修改 payload
// 后批准 → 卡片生效; 以及同 Key 重试不重复创建.
func TestThirdBatchAcceptance(t *testing.T) {
	ts := newTestServer(t)
	cli := ts.newCLIClient(t)
	csrf := ts.login()

	// 1. CLI 建 Topic 提案.
	topicApprovalID := approveCLI(t, cli, http.MethodPost, "/api/cli/topics",
		map[string]any{"name": "Go", "description": "Go 语言"}, "p-topic")
	// 审批开启时 CLI 列表可见.
	resp := cli.do(http.MethodGet, "/api/cli/approvals?status=pending", nil, "")
	var cliList struct {
		Items []approvalDetail `json:"items"`
		Total int64            `json:"total"`
	}
	decodeBody(t, resp, &cliList)
	if cliList.Total != 1 || cliList.Items[0].Operation != "topic_create" {
		t.Fatalf("cli approvals = %+v", cliList)
	}

	// 2. 同 Key 同请求重试: 返回同一提案, 不重复创建.
	resp = cli.do(http.MethodPost, "/api/cli/topics",
		map[string]any{"name": "Go", "description": "Go 语言"}, "p-topic")
	var replay struct {
		Approval approvalSummary `json:"approval"`
	}
	decodeBody(t, resp, &replay)
	if replay.Approval.ID != topicApprovalID {
		t.Fatalf("replayed approval id = %d, want %d", replay.Approval.ID, topicApprovalID)
	}
	resp = ts.do(http.MethodGet, "/api/web/approvals", nil, nil)
	var webList struct {
		Total int64 `json:"total"`
	}
	decodeBody(t, resp, &webList)
	if webList.Total != 1 {
		t.Fatalf("approvals total after retry = %d, want 1", webList.Total)
	}

	// 3. Web 批准 Topic 提案.
	resp = ts.do(http.MethodPost, fmt.Sprintf("/api/web/approvals/%d/approve", topicApprovalID),
		map[string]any{}, map[string]string{"X-CSRF-Token": csrf})
	var approved approvalDetail
	decodeBody(t, resp, &approved)
	if resp.StatusCode != http.StatusOK || approved.Status != "approved" {
		t.Fatalf("approve topic status = %d / %s", resp.StatusCode, approved.Status)
	}

	// 4. 取回 Topic ID.
	resp = ts.do(http.MethodGet, "/api/web/topics", nil, nil)
	var topics struct {
		Items []struct {
			ID      int64 `json:"id"`
			Version int64 `json:"version"`
		} `json:"items"`
	}
	decodeBody(t, resp, &topics)
	if len(topics.Items) != 1 {
		t.Fatalf("topics = %+v", topics)
	}
	topicID := topics.Items[0].ID

	// 5. CLI 建卡提案, 目标引用该 Topic.
	cardApprovalID := approveCLI(t, cli, http.MethodPost, "/api/cli/cards",
		map[string]any{"topic_id": topicID, "front": "原 front", "back": "原 back"}, "p-card")

	// 详情应登记主对象与 topic 引用两个 target.
	resp = ts.do(http.MethodGet, fmt.Sprintf("/api/web/approvals/%d", cardApprovalID), nil, nil)
	var detail approvalDetail
	decodeBody(t, resp, &detail)
	if len(detail.Targets) != 1 {
		// 建卡无主对象; topic 引用是唯一 target.
		t.Fatalf("card_create targets = %+v, want 1 topic target", detail.Targets)
	}
	if detail.Targets[0].Role != "topic" || detail.Targets[0].EntityID != topicID {
		t.Fatalf("topic target = %+v", detail.Targets[0])
	}

	// 6. Web 修改 payload 后批准: 卡片按修改后的内容创建.
	resp = ts.do(http.MethodPost, fmt.Sprintf("/api/web/approvals/%d/approve", cardApprovalID),
		map[string]any{"payload": map[string]any{
			"topic_id": topicID, "front": "修改后的 front", "back": "修改后的 back",
		}}, map[string]string{"X-CSRF-Token": csrf})
	var cardApproved approvalDetail
	decodeBody(t, resp, &cardApproved)
	if resp.StatusCode != http.StatusOK || cardApproved.Status != "approved" {
		t.Fatalf("approve card status = %d / %s", resp.StatusCode, cardApproved.Status)
	}
	if cardApproved.ApprovedPayload == nil {
		t.Fatal("approved_payload should be saved")
	}

	resp = ts.do(http.MethodGet, "/api/web/cards", nil, nil)
	var cards struct {
		Items []struct {
			ID    int64  `json:"id"`
			Front string `json:"front"`
			Back  string `json:"back"`
		} `json:"items"`
	}
	decodeBody(t, resp, &cards)
	if len(cards.Items) != 1 || cards.Items[0].Front != "修改后的 front" ||
		cards.Items[0].Back != "修改后的 back" {
		t.Fatalf("card after approve = %+v", cards.Items)
	}

	// 7. 重复批准返回 422 approval_not_pending.
	resp = ts.do(http.MethodPost, fmt.Sprintf("/api/web/approvals/%d/approve", cardApprovalID),
		map[string]any{}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("re-approve status = %d, want 422", resp.StatusCode)
	}
	var errBody struct {
		Error struct {
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	decodeBody(t, resp, &errBody)
	if errBody.Error.Code != "approval_not_pending" {
		t.Fatalf("error code = %s, want approval_not_pending", errBody.Error.Code)
	}
}

// TestApprovalStaleScenarios 覆盖: target 版本被并发修改后批准转 stale;
// 对象被回收时 pending 提案立即 stale.
func TestApprovalStaleScenarios(t *testing.T) {
	ts := newTestServer(t)
	cli := ts.newCLIClient(t)
	csrf := ts.login()

	// 直写准备基础数据.
	disableCLIApprovals(t, ts, csrf)
	resp := cli.do(http.MethodPost, "/api/cli/topics", map[string]any{"name": "T"}, "t1")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create topic status = %d", resp.StatusCode)
	}
	var topic struct {
		ID      int64 `json:"id"`
		Version int64 `json:"version"`
	}
	decodeBody(t, resp, &topic)
	resp = cli.do(http.MethodPost, "/api/cli/cards", map[string]any{
		"topic_id": topic.ID, "front": "f", "back": "b",
	}, "c1")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create card status = %d", resp.StatusCode)
	}
	var card struct {
		ID      int64 `json:"id"`
		Version int64 `json:"version"`
	}
	decodeBody(t, resp, &card)

	// 重新开启卡更新审批, 提交改卡提案.
	enableOneApproval(t, ts, csrf, "enable_cli_card_update_approval", true)
	updateID := approveCLI(t, cli, http.MethodPatch, fmt.Sprintf("/api/cli/cards/%d", card.ID),
		map[string]any{"expected_version": card.Version, "back": "提案内容"}, "u1")

	// 直接用 Web 改卡, 使提案 base_version 过期.
	resp = ts.do(http.MethodPatch, fmt.Sprintf("/api/web/cards/%d", card.ID), map[string]any{
		"expected_version": card.Version, "back": "并发修改",
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("web update card status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 批准应转 stale, 卡片保持并发修改后的内容.
	resp = ts.do(http.MethodPost, fmt.Sprintf("/api/web/approvals/%d/approve", updateID),
		map[string]any{}, map[string]string{"X-CSRF-Token": csrf})
	var stale approvalDetail
	decodeBody(t, resp, &stale)
	if resp.StatusCode != http.StatusOK || stale.Status != "stale" {
		t.Fatalf("stale approve = %d / %s", resp.StatusCode, stale.Status)
	}
	if stale.Reason == nil || *stale.Reason == "" {
		t.Fatal("stale reason should not be empty")
	}
	resp = ts.do(http.MethodGet, fmt.Sprintf("/api/web/cards/%d", card.ID), nil, nil)
	var after struct {
		Back string `json:"back"`
	}
	decodeBody(t, resp, &after)
	if after.Back != "并发修改" {
		t.Fatalf("card back = %s, want 并发修改", after.Back)
	}

	// 对象被回收时 pending 提案立即 stale.
	enableOneApproval(t, ts, csrf, "enable_cli_card_update_approval", true)
	pendingID := approveCLI(t, cli, http.MethodPatch, fmt.Sprintf("/api/cli/cards/%d", card.ID),
		map[string]any{"expected_version": 2, "back": "会被回收的提案"}, "u2")
	resp = ts.do(http.MethodPost, fmt.Sprintf("/api/web/cards/%d/trash", card.ID), map[string]any{
		"expected_version": 2,
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("trash card status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = ts.do(http.MethodGet, fmt.Sprintf("/api/web/approvals/%d", pendingID), nil, nil)
	var autoStale approvalDetail
	decodeBody(t, resp, &autoStale)
	if autoStale.Status != "stale" {
		t.Fatalf("pending approval after trash = %s, want stale", autoStale.Status)
	}
	if autoStale.Reason == nil || *autoStale.Reason != "target_trashed" {
		t.Fatalf("stale reason = %v, want target_trashed", autoStale.Reason)
	}
}

// TestApprovalRejectAndBatch 覆盖拒绝与批量批准/拒绝的逐条结果.
func TestApprovalRejectAndBatch(t *testing.T) {
	ts := newTestServer(t)
	cli := ts.newCLIClient(t)
	csrf := ts.login()

	// 三条 Topic 提案 (审批默认开启).
	ids := make([]int64, 0, 3)
	for i := 0; i < 3; i++ {
		ids = append(ids, approveCLI(t, cli, http.MethodPost, "/api/cli/topics",
			map[string]any{"name": fmt.Sprintf("批量 Topic %d", i)}, fmt.Sprintf("b-%d", i)))
	}

	// 拒绝一条.
	resp := ts.do(http.MethodPost, fmt.Sprintf("/api/web/approvals/%d/reject", ids[0]),
		map[string]any{"reason": "不需要"}, map[string]string{"X-CSRF-Token": csrf})
	var rejected approvalDetail
	decodeBody(t, resp, &rejected)
	if resp.StatusCode != http.StatusOK || rejected.Status != "rejected" {
		t.Fatalf("reject = %d / %s", resp.StatusCode, rejected.Status)
	}
	if rejected.Reason == nil || *rejected.Reason != "不需要" {
		t.Fatalf("reject reason = %v", rejected.Reason)
	}

	// 批量批准剩下两条 + 一条已拒绝的 (应报 approval_not_pending).
	resp = ts.do(http.MethodPost, "/api/web/approvals/batch-approve",
		map[string]any{"ids": []int64{ids[1], ids[2], ids[0]}},
		map[string]string{"X-CSRF-Token": csrf})
	var batch struct {
		Results []struct {
			ID      int64 `json:"id"`
			Success bool  `json:"success"`
			Error   *struct {
				Code string `json:"code"`
			} `json:"error"`
		} `json:"results"`
	}
	decodeBody(t, resp, &batch)
	if resp.StatusCode != http.StatusOK || len(batch.Results) != 3 {
		t.Fatalf("batch approve = %d / %+v", resp.StatusCode, batch)
	}
	if !batch.Results[0].Success || !batch.Results[1].Success {
		t.Fatalf("first two should succeed: %+v", batch.Results)
	}
	if batch.Results[2].Success || batch.Results[2].Error == nil ||
		batch.Results[2].Error.Code != "approval_not_pending" {
		t.Fatalf("third result = %+v", batch.Results[2])
	}

	// 两批 Topic 已创建.
	resp = ts.do(http.MethodGet, "/api/web/topics", nil, nil)
	var topics struct {
		Total int64 `json:"total"`
	}
	decodeBody(t, resp, &topics)
	if topics.Total != 2 {
		t.Fatalf("topics total = %d, want 2", topics.Total)
	}
}

// TestSettingsEndpoints 覆盖 GET/PATCH /api/web/settings 的范围与类型校验.
func TestSettingsEndpoints(t *testing.T) {
	ts := newTestServer(t)
	csrf := ts.login()

	// GET 返回审批开关与日志设置, 值类型正确.
	resp := ts.do(http.MethodGet, "/api/web/settings", nil, nil)
	var got struct {
		Settings map[string]any `json:"settings"`
	}
	decodeBody(t, resp, &got)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get settings status = %d", resp.StatusCode)
	}
	if _, ok := got.Settings["enable_cli_card_create_approval"].(bool); !ok {
		t.Fatalf("approval switch type = %T", got.Settings["enable_cli_card_create_approval"])
	}
	if _, ok := got.Settings["database_max_rows"].(float64); !ok {
		t.Fatalf("int setting type = %T", got.Settings["database_max_rows"])
	}
	if _, ok := got.Settings["calendar_timezone"].(string); !ok {
		t.Fatalf("string setting type = %T", got.Settings["calendar_timezone"])
	}
	// embedding 设置不在通用端点内.
	if _, ok := got.Settings["embedding_base_url"]; ok {
		t.Fatal("embedding settings should not be exposed by /api/web/settings")
	}

	// PATCH 合法修改.
	resp = ts.do(http.MethodPatch, "/api/web/settings",
		map[string]any{"enable_cli_card_create_approval": false, "database_max_rows": 5000},
		map[string]string{"X-CSRF-Token": csrf})
	var patched struct {
		Settings map[string]any `json:"settings"`
	}
	decodeBody(t, resp, &patched)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch settings status = %d", resp.StatusCode)
	}
	if patched.Settings["enable_cli_card_create_approval"] != false {
		t.Fatalf("approval switch after patch = %v", patched.Settings["enable_cli_card_create_approval"])
	}

	// 未知 key 返回 400.
	resp = ts.do(http.MethodPatch, "/api/web/settings",
		map[string]any{"not_a_key": true}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("patch unknown key status = %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()

	// 类型不匹配返回 400.
	resp = ts.do(http.MethodPatch, "/api/web/settings",
		map[string]any{"enable_cli_card_create_approval": "yes"},
		map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("patch wrong type status = %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()

	// 范围外 key (embedding) 被拒绝.
	resp = ts.do(http.MethodPatch, "/api/web/settings",
		map[string]any{"embedding_base_url": "http://x"}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("patch out-of-scope key status = %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestApprovalCLIOScope 验证 CLI 只能看到自己的提案.
func TestApprovalCLIOScope(t *testing.T) {
	ts := newTestServer(t)
	cliA := ts.newCLIClient(t)
	cliB := ts.newCLIClient(t)

	id := approveCLI(t, cliA, http.MethodPost, "/api/cli/topics",
		map[string]any{"name": "A 的提案"}, "scope-1")

	// A 能读, B 读不到 (404).
	resp := cliA.do(http.MethodGet, fmt.Sprintf("/api/cli/approvals/%d", id), nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("owner get status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = cliB.do(http.MethodGet, fmt.Sprintf("/api/cli/approvals/%d", id), nil, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("other get status = %d, want 404", resp.StatusCode)
	}
	resp.Body.Close()
	// B 的列表为空.
	resp = cliB.do(http.MethodGet, "/api/cli/approvals", nil, "")
	var list struct {
		Total int64 `json:"total"`
	}
	decodeBody(t, resp, &list)
	if list.Total != 0 {
		t.Fatalf("other key approvals = %d, want 0", list.Total)
	}
}

// TestApprovalBatchProposal 覆盖批量提案: 一个事务中每个项目独立提案,
// 任一不合法整批不创建.
func TestApprovalBatchProposal(t *testing.T) {
	ts := newTestServer(t)
	cli := ts.newCLIClient(t)

	// 合法批量: 两条提案.
	resp := cli.do(http.MethodPost, "/api/cli/topics/batch-create", map[string]any{
		"items": []map[string]any{{"name": "B1"}, {"name": "B2"}},
	}, "batch-ok")
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("batch propose status = %d, want 202", resp.StatusCode)
	}
	var out struct {
		Approvals []approvalSummary `json:"approvals"`
	}
	decodeBody(t, resp, &out)
	if len(out.Approvals) != 2 {
		t.Fatalf("batch approvals = %+v", out.Approvals)
	}

	// 任一非法整批不创建: 第二条空名.
	resp = cli.do(http.MethodPost, "/api/cli/topics/batch-create", map[string]any{
		"items": []map[string]any{{"name": "C1"}, {"name": "  "}},
	}, "batch-bad")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("batch invalid status = %d, want 400", resp.StatusCode)
	}
	var errBody struct {
		Error struct {
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	decodeBody(t, resp, &errBody)
	if errBody.Error.Code != "validation_error" || errBody.Error.Details["index"] != float64(1) {
		t.Fatalf("batch invalid error = %+v", errBody.Error)
	}
	// 整批未创建: 仍只有上一批的两条.
	resp = ts.do(http.MethodGet, "/api/web/approvals", nil, nil)
	var list struct {
		Total int64 `json:"total"`
	}
	decodeBody(t, resp, &list)
	if list.Total != 2 {
		t.Fatalf("approvals after invalid batch = %d, want 2", list.Total)
	}
}

// enableOneApproval 修改单个审批开关.
func enableOneApproval(t *testing.T, ts *testServer, csrf, key string, value bool) {
	t.Helper()
	resp := ts.do(http.MethodPatch, "/api/web/settings",
		map[string]any{key: value}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch %s status = %d", key, resp.StatusCode)
	}
	resp.Body.Close()
}

// TestApprovalTopicTrashWithCards 覆盖 Topic 连带回收提案: targets 含主
// Topic 与全部关联卡; 任一关联卡变化都会让请求 stale.
func TestApprovalTopicTrashWithCards(t *testing.T) {
	ts := newTestServer(t)
	cli := ts.newCLIClient(t)
	csrf := ts.login()
	disableCLIApprovals(t, ts, csrf)

	resp := cli.do(http.MethodPost, "/api/cli/topics", map[string]any{"name": "含卡 Topic"}, "tt-t")
	var topic struct {
		ID      int64 `json:"id"`
		Version int64 `json:"version"`
	}
	decodeBody(t, resp, &topic)
	cardIDs := make([]int64, 0, 2)
	for i := 0; i < 2; i++ {
		resp = cli.do(http.MethodPost, "/api/cli/cards", map[string]any{
			"topic_id": topic.ID, "front": fmt.Sprintf("f%d", i), "back": "b",
		}, fmt.Sprintf("tt-c%d", i))
		var card struct {
			ID int64 `json:"id"`
		}
		decodeBody(t, resp, &card)
		cardIDs = append(cardIDs, card.ID)
	}

	enableOneApproval(t, ts, csrf, "enable_cli_topic_trash_approval", true)
	id := approveCLI(t, cli, http.MethodPost, fmt.Sprintf("/api/cli/topics/%d/trash", topic.ID),
		map[string]any{"expected_version": topic.Version, "include_cards": true}, "tt-1")

	// 详情含 1 个主 target + 2 个 affected_card.
	resp = ts.do(http.MethodGet, fmt.Sprintf("/api/web/approvals/%d", id), nil, nil)
	var detail approvalDetail
	decodeBody(t, resp, &detail)
	if len(detail.Targets) != 3 {
		t.Fatalf("topic_trash targets = %+v, want 3", detail.Targets)
	}
	var affected, primary int
	for _, tg := range detail.Targets {
		switch tg.Role {
		case "affected_card":
			affected++
		case "target":
			primary++
		}
	}
	if primary != 1 || affected != 2 {
		t.Fatalf("roles = %+v", detail.Targets)
	}

	// 批准后 Topic 与两张卡都进回收站.
	resp = ts.do(http.MethodPost, fmt.Sprintf("/api/web/approvals/%d/approve", id),
		map[string]any{}, map[string]string{"X-CSRF-Token": csrf})
	var approved approvalDetail
	decodeBody(t, resp, &approved)
	if approved.Status != "approved" {
		t.Fatalf("approve status = %s", approved.Status)
	}
	resp = ts.do(http.MethodGet, "/api/web/trash/topics", nil, nil)
	var trashedTopics struct {
		Total int64 `json:"total"`
	}
	decodeBody(t, resp, &trashedTopics)
	if trashedTopics.Total != 1 {
		t.Fatalf("trashed topics = %d, want 1", trashedTopics.Total)
	}
	resp = ts.do(http.MethodGet, "/api/web/trash/cards", nil, nil)
	var trashedCards struct {
		Total int64 `json:"total"`
	}
	decodeBody(t, resp, &trashedCards)
	if trashedCards.Total != 2 {
		t.Fatalf("trashed cards = %d, want 2", trashedCards.Total)
	}
}

// TestApprovalGlossaryFlow 覆盖 Glossary 四类提案的批准执行.
func TestApprovalGlossaryFlow(t *testing.T) {
	ts := newTestServer(t)
	cli := ts.newCLIClient(t)
	csrf := ts.login()

	// 建.
	createID := approveCLI(t, cli, http.MethodPost, "/api/cli/glossary",
		map[string]any{"term": "goroutine", "definition": "轻量线程"}, "g-create")
	resp := ts.do(http.MethodPost, fmt.Sprintf("/api/web/approvals/%d/approve", createID),
		map[string]any{}, map[string]string{"X-CSRF-Token": csrf})
	var created approvalDetail
	decodeBody(t, resp, &created)
	if created.Status != "approved" {
		t.Fatalf("glossary create approve = %s", created.Status)
	}
	resp = ts.do(http.MethodGet, "/api/web/glossary", nil, nil)
	var list struct {
		Items []struct {
			ID      int64  `json:"id"`
			Term    string `json:"term"`
			Version int64  `json:"version"`
		} `json:"items"`
	}
	decodeBody(t, resp, &list)
	if len(list.Items) != 1 {
		t.Fatalf("glossary list = %+v", list.Items)
	}
	g := list.Items[0]

	// 改.
	updateID := approveCLI(t, cli, http.MethodPatch, fmt.Sprintf("/api/cli/glossary/%d", g.ID),
		map[string]any{"expected_version": g.Version, "definition": "更新后的定义"}, "g-update")
	resp = ts.do(http.MethodPost, fmt.Sprintf("/api/web/approvals/%d/approve", updateID),
		map[string]any{}, map[string]string{"X-CSRF-Token": csrf})
	var updated approvalDetail
	decodeBody(t, resp, &updated)
	if updated.Status != "approved" {
		t.Fatalf("glossary update approve = %s", updated.Status)
	}

	// 回收.
	trashID := approveCLI(t, cli, http.MethodPost, fmt.Sprintf("/api/cli/glossary/%d/trash", g.ID),
		map[string]any{"expected_version": 2}, "g-trash")
	resp = ts.do(http.MethodPost, fmt.Sprintf("/api/web/approvals/%d/approve", trashID),
		map[string]any{}, map[string]string{"X-CSRF-Token": csrf})
	var trashed approvalDetail
	decodeBody(t, resp, &trashed)
	if trashed.Status != "approved" {
		t.Fatalf("glossary trash approve = %s", trashed.Status)
	}

	// 恢复.
	restoreID := approveCLI(t, cli, http.MethodPost, fmt.Sprintf("/api/cli/trash/glossary/%d/restore", g.ID),
		map[string]any{"expected_version": 3}, "g-restore")
	resp = ts.do(http.MethodPost, fmt.Sprintf("/api/web/approvals/%d/approve", restoreID),
		map[string]any{}, map[string]string{"X-CSRF-Token": csrf})
	var restored approvalDetail
	decodeBody(t, resp, &restored)
	if restored.Status != "approved" {
		t.Fatalf("glossary restore approve = %s", restored.Status)
	}
	resp = ts.do(http.MethodGet, "/api/web/glossary", nil, nil)
	var after struct {
		Total int64 `json:"total"`
	}
	decodeBody(t, resp, &after)
	if after.Total != 1 {
		t.Fatalf("glossary total after restore = %d, want 1", after.Total)
	}
}
