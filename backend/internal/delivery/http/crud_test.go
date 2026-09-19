package httpapi_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
)

// cliClient 是无 cookie 的客户端, 只携带 API Key 与幂等头,
// 保证 CLI 命名空间的认证语义不被 Web session 干扰.
type cliClient struct {
	ts     *testServer
	apiKey string
}

// newCLIClient 登录 Web 创建一把 API Key, 返回 CLI 客户端.
func (ts *testServer) newCLIClient(t *testing.T) *cliClient {
	t.Helper()
	csrf := ts.login()
	resp := ts.do(http.MethodPost, "/api/web/api-keys",
		map[string]string{"name": "cli-test"},
		map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create api key status = %d, want 201", resp.StatusCode)
	}
	var created struct {
		APIKey string `json:"api_key"`
	}
	decodeBody(t, resp, &created)
	return &cliClient{ts: ts, apiKey: created.APIKey}
}

// do 发起 CLI 请求: Bearer 认证, 写请求自动携带幂等头.
// 使用无 cookie 的裸客户端, 保证 CLI 命名空间的认证语义
// 不被 Web session 干扰.
func (c *cliClient) do(method, path string, body any, idempotencyKey string) *http.Response {
	c.ts.t.Helper()
	headers := map[string]string{"Authorization": "Bearer " + c.apiKey}
	if idempotencyKey != "" {
		headers["Idempotency-Key"] = idempotencyKey
	}
	return doRequest(c.ts.t, http.DefaultClient, c.ts.srv.URL+path, method, body, headers)
}

// doRequest 用指定客户端发起 JSON 请求.
func doRequest(t *testing.T, client *http.Client, url, method string, body any, headers map[string]string) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

// TestSecondBatchWebAcceptance 覆盖第二批 Web 验收路径:
// 建 Topic → 建卡 → 改卡 → 回收 → 恢复 → 永久删除.
func TestSecondBatchWebAcceptance(t *testing.T) {
	ts := newTestServer(t)
	csrf := ts.login()

	// 1. 建 Topic.
	resp := ts.do(http.MethodPost, "/api/web/topics", map[string]any{
		"name": "Go 语言", "description": "Go 相关知识",
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create topic status = %d, want 201", resp.StatusCode)
	}
	var topic struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		CardCount int64  `json:"card_count"`
		Version   int64  `json:"version"`
	}
	decodeBody(t, resp, &topic)
	if topic.ID == 0 || topic.Version != 1 || topic.CardCount != 0 {
		t.Fatalf("unexpected topic: %+v", topic)
	}

	// 2. 列表可见, card_count 计算正确.
	resp = ts.do(http.MethodGet, "/api/web/topics", nil, nil)
	var topicList struct {
		Items []struct {
			ID        int64 `json:"id"`
			CardCount int64 `json:"card_count"`
		} `json:"items"`
		Total int64 `json:"total"`
	}
	decodeBody(t, resp, &topicList)
	if topicList.Total != 1 || len(topicList.Items) != 1 {
		t.Fatalf("topic list = %+v", topicList)
	}

	// 3. 建卡: 详情含新卡调度.
	resp = ts.do(http.MethodPost, "/api/web/cards", map[string]any{
		"topic_id": topic.ID,
		"front":    "Go slice 的底层结构是什么?",
		"back":     "slice header 包含指针、长度和容量.",
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create card status = %d, want 201", resp.StatusCode)
	}
	var card struct {
		ID              int64  `json:"id"`
		TopicID         *int64 `json:"topic_id"`
		Version         int64  `json:"version"`
		EmbeddingStatus string `json:"embedding_status"`
		Schedule        *struct {
			State   string `json:"state"`
			Version int64  `json:"version"`
			Reps    int64  `json:"reps"`
		} `json:"schedule"`
	}
	decodeBody(t, resp, &card)
	if card.TopicID == nil || *card.TopicID != topic.ID {
		t.Fatalf("card topic_id = %v", card.TopicID)
	}
	if card.Version != 1 || card.EmbeddingStatus != "pending" {
		t.Fatalf("card version/status = %d/%s", card.Version, card.EmbeddingStatus)
	}
	if card.Schedule == nil || card.Schedule.State != "new" || card.Schedule.Version != 1 {
		t.Fatalf("card schedule = %+v", card.Schedule)
	}

	// 4. Topic 详情的 card_count 变为 1.
	resp = ts.do(http.MethodGet, fmt.Sprintf("/api/web/topics/%d", topic.ID), nil, nil)
	var topicDetail struct {
		CardCount int64 `json:"card_count"`
	}
	decodeBody(t, resp, &topicDetail)
	if topicDetail.CardCount != 1 {
		t.Fatalf("card_count = %d, want 1", topicDetail.CardCount)
	}

	// 5. 改卡: front 变化递增 version.
	resp = ts.do(http.MethodPatch, fmt.Sprintf("/api/web/cards/%d", card.ID), map[string]any{
		"expected_version": card.Version,
		"front":            "Go slice 底层结构?",
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update card status = %d, want 200", resp.StatusCode)
	}
	var updated struct {
		Version int64  `json:"version"`
		Front   string `json:"front"`
	}
	decodeBody(t, resp, &updated)
	if updated.Version != 2 || updated.Front != "Go slice 底层结构?" {
		t.Fatalf("updated card = %+v", updated)
	}

	// 6. 旧版本号修改返回 409 并携带 current_version.
	resp = ts.do(http.MethodPatch, fmt.Sprintf("/api/web/cards/%d", card.ID), map[string]any{
		"expected_version": 1,
		"back":             "过期版本",
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("stale update status = %d, want 409", resp.StatusCode)
	}
	var conflict struct {
		Error struct {
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	decodeBody(t, resp, &conflict)
	if conflict.Error.Code != "version_conflict" {
		t.Fatalf("conflict code = %s", conflict.Error.Code)
	}
	if v, ok := conflict.Error.Details["current_version"].(float64); !ok || int64(v) != 2 {
		t.Fatalf("conflict details = %+v", conflict.Error.Details)
	}

	// 7. 回收: 返回 trashed_card_id, 详情变 404.
	resp = ts.do(http.MethodPost, fmt.Sprintf("/api/web/cards/%d/trash", card.ID), map[string]any{
		"expected_version": 2,
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("trash card status = %d, want 200", resp.StatusCode)
	}
	var trashed struct {
		TrashedCardID int64 `json:"trashed_card_id"`
	}
	decodeBody(t, resp, &trashed)
	if trashed.TrashedCardID != card.ID {
		t.Fatalf("trashed_card_id = %d, want %d", trashed.TrashedCardID, card.ID)
	}
	resp = ts.do(http.MethodGet, fmt.Sprintf("/api/web/cards/%d", card.ID), nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get trashed card status = %d, want 404", resp.StatusCode)
	}
	resp.Body.Close()

	// 8. 回收站列表可见.
	resp = ts.do(http.MethodGet, "/api/web/trash/cards", nil, nil)
	var trashList struct {
		Items []struct {
			ID      int64  `json:"id"`
			Front   string `json:"front"`
			Version int64  `json:"version"`
		} `json:"items"`
		Total int64 `json:"total"`
	}
	decodeBody(t, resp, &trashList)
	if trashList.Total != 1 || len(trashList.Items) != 1 {
		t.Fatalf("trash list = %+v", trashList)
	}
	if trashList.Items[0].Version != 3 {
		t.Fatalf("trashed card version = %d, want 3 (原 version+1)", trashList.Items[0].Version)
	}

	// 9. 恢复: 新 ID, 新调度, version 重置为 1.
	resp = ts.do(http.MethodPost, fmt.Sprintf("/api/web/trash/cards/%d/restore", trashed.TrashedCardID), map[string]any{
		"expected_version": 3,
		"topic_id":         topic.ID,
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("restore card status = %d, want 200", resp.StatusCode)
	}
	var restored struct {
		TrashCardID int64 `json:"trash_card_id"`
		NewCardID   int64 `json:"new_card_id"`
	}
	decodeBody(t, resp, &restored)
	if restored.TrashCardID != card.ID || restored.NewCardID == card.ID || restored.NewCardID == 0 {
		t.Fatalf("restore result = %+v", restored)
	}
	resp = ts.do(http.MethodGet, fmt.Sprintf("/api/web/cards/%d", restored.NewCardID), nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get restored card status = %d", resp.StatusCode)
	}
	var restoredCard struct {
		Version  int64 `json:"version"`
		Schedule *struct {
			State   string `json:"state"`
			Version int64  `json:"version"`
		} `json:"schedule"`
	}
	decodeBody(t, resp, &restoredCard)
	if restoredCard.Version != 1 || restoredCard.Schedule == nil ||
		restoredCard.Schedule.State != "new" || restoredCard.Schedule.Version != 1 {
		t.Fatalf("restored card = %+v", restoredCard)
	}

	// 10. 永久删除: 先重新回收新卡, 再从回收站永久删除.
	resp = ts.do(http.MethodPost, fmt.Sprintf("/api/web/cards/%d/trash", restored.NewCardID), map[string]any{
		"expected_version": 1,
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("re-trash status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()
	resp = ts.do(http.MethodPost, fmt.Sprintf("/api/web/trash/cards/%d/delete", restored.NewCardID), map[string]any{
		"expected_version": 2,
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete forever status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()
	resp = ts.do(http.MethodGet, "/api/web/trash/cards", nil, nil)
	decodeBody(t, resp, &trashList)
	if trashList.Total != 0 {
		t.Fatalf("trash list after delete = %+v", trashList)
	}
}

// TestCLIDirectWriteAndIdempotencyHeader 覆盖 CLI 直写验收路径:
// 关闭审批后直写生效 + 幂等头强制 + 同 Key 重放 + 永久删除不开放.
func TestCLIDirectWriteAndIdempotencyHeader(t *testing.T) {
	ts := newTestServer(t)
	cli := ts.newCLIClient(t)

	// 0. 关闭全部 CLI 审批开关, 走直写路径 (审批开启时的提案路径由
	//    approvals_test.go 覆盖).
	csrf := ts.login()
	disableCLIApprovals(t, ts, csrf)

	// 1. 缺少 Idempotency-Key 的写请求被拒绝.
	resp := cli.do(http.MethodPost, "/api/cli/topics",
		map[string]any{"name": "CLI Topic"}, "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("cli write without idempotency key status = %d, want 400", resp.StatusCode)
	}
	var errBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decodeBody(t, resp, &errBody)
	if errBody.Error.Code != "validation_error" {
		t.Fatalf("error code = %s", errBody.Error.Code)
	}

	// 2. 携带幂等头后直写生效 (201, 不是 202 提案).
	resp = cli.do(http.MethodPost, "/api/cli/topics",
		map[string]any{"name": "CLI Topic"}, "idem-1")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("cli create topic status = %d, want 201", resp.StatusCode)
	}
	var topic struct {
		ID      int64 `json:"id"`
		Version int64 `json:"version"`
	}
	decodeBody(t, resp, &topic)

	// 2b. 同 Key 同请求重放首次结果, 不重复创建.
	resp = cli.do(http.MethodPost, "/api/cli/topics",
		map[string]any{"name": "CLI Topic"}, "idem-1")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("cli replay status = %d, want 201", resp.StatusCode)
	}
	var replayed struct {
		ID int64 `json:"id"`
	}
	decodeBody(t, resp, &replayed)
	if replayed.ID != topic.ID {
		t.Fatalf("replayed topic id = %d, want %d", replayed.ID, topic.ID)
	}

	// 2c. 同 Key 不同请求返回 409 idempotency_conflict.
	resp = cli.do(http.MethodPost, "/api/cli/topics",
		map[string]any{"name": "另一个名字"}, "idem-1")
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("cli different-body same-key status = %d, want 409", resp.StatusCode)
	}
	decodeBody(t, resp, &errBody)
	if errBody.Error.Code != "idempotency_conflict" {
		t.Fatalf("error code = %s, want idempotency_conflict", errBody.Error.Code)
	}

	// 3. CLI 建卡与改卡.
	resp = cli.do(http.MethodPost, "/api/cli/cards", map[string]any{
		"topic_id": topic.ID,
		"front":    "CLI 建的卡",
		"back":     "直写生效",
	}, "idem-2")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("cli create card status = %d, want 201", resp.StatusCode)
	}
	var card struct {
		ID      int64 `json:"id"`
		Version int64 `json:"version"`
	}
	decodeBody(t, resp, &card)
	resp = cli.do(http.MethodPatch, fmt.Sprintf("/api/cli/cards/%d", card.ID), map[string]any{
		"expected_version": card.Version,
		"back":             "CLI 改的卡",
	}, "idem-3")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cli update card status = %d, want 200", resp.StatusCode)
	}

	// 4. CLI 回收与恢复.
	resp = cli.do(http.MethodPost, fmt.Sprintf("/api/cli/cards/%d/trash", card.ID), map[string]any{
		"expected_version": 2,
	}, "idem-4")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cli trash card status = %d, want 200", resp.StatusCode)
	}
	resp = cli.do(http.MethodPost, fmt.Sprintf("/api/cli/trash/cards/%d/restore", card.ID), map[string]any{
		"expected_version": 3,
	}, "idem-5")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cli restore card status = %d, want 200", resp.StatusCode)
	}

	// 5. CLI 永久删除路由不存在.
	resp = cli.do(http.MethodPost, fmt.Sprintf("/api/cli/trash/cards/%d/delete", card.ID), map[string]any{
		"expected_version": 1,
	}, "idem-6")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cli delete forever status = %d, want 404", resp.StatusCode)
	}
	resp.Body.Close()
}

// disableCLIApprovals 通过 PATCH /api/web/settings 关闭全部 CLI 审批开关.
func disableCLIApprovals(t *testing.T, ts *testServer, csrf string) {
	t.Helper()
	patch := map[string]any{}
	for _, key := range []string{
		"enable_cli_card_create_approval",
		"enable_cli_card_update_approval",
		"enable_cli_card_trash_approval",
		"enable_cli_card_restore_approval",
		"enable_cli_card_merge_approval",
		"enable_cli_topic_create_approval",
		"enable_cli_topic_update_approval",
		"enable_cli_topic_trash_approval",
		"enable_cli_topic_restore_approval",
		"enable_cli_glossary_create_approval",
		"enable_cli_glossary_update_approval",
		"enable_cli_glossary_trash_approval",
		"enable_cli_glossary_restore_approval",
	} {
		patch[key] = false
	}
	resp := ts.do(http.MethodPatch, "/api/web/settings", patch,
		map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch settings status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestTopicNameConflictAndOverwrite 验证名称唯一与回收站同名覆盖.
func TestTopicNameConflictAndOverwrite(t *testing.T) {
	ts := newTestServer(t)
	csrf := ts.login()

	create := func(name string) *http.Response {
		return ts.do(http.MethodPost, "/api/web/topics", map[string]any{"name": name},
			map[string]string{"X-CSRF-Token": csrf})
	}

	// 正常同名冲突.
	resp := create("Go")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("first create status = %d", resp.StatusCode)
	}
	var first struct {
		ID int64 `json:"id"`
	}
	decodeBody(t, resp, &first)
	resp = create("Go")
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate create status = %d, want 409", resp.StatusCode)
	}
	var conflict struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decodeBody(t, resp, &conflict)
	if conflict.Error.Code != "name_conflict" {
		t.Fatalf("code = %s, want name_conflict", conflict.Error.Code)
	}

	// 回收后同名可重建, 回收站记录被覆盖删除.
	resp = ts.do(http.MethodPost, fmt.Sprintf("/api/web/topics/%d/trash", first.ID), map[string]any{
		"expected_version": 1,
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("trash status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = create("Go")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("recreate after trash status = %d, want 201", resp.StatusCode)
	}
	resp.Body.Close()
	resp = ts.do(http.MethodGet, "/api/web/trash/topics", nil, nil)
	var trashList struct {
		Total int64 `json:"total"`
	}
	decodeBody(t, resp, &trashList)
	if trashList.Total != 0 {
		t.Fatalf("trashed topics after overwrite = %d, want 0", trashList.Total)
	}
}

// TestTopicTrashIncludeCards 验证 Topic 回收的关联卡两种处理与预览.
func TestTopicTrashIncludeCards(t *testing.T) {
	ts := newTestServer(t)
	csrf := ts.login()

	// 建 Topic 与两张卡.
	resp := ts.do(http.MethodPost, "/api/web/topics", map[string]any{"name": "T"},
		map[string]string{"X-CSRF-Token": csrf})
	var topic struct {
		ID int64 `json:"id"`
	}
	decodeBody(t, resp, &topic)
	cardIDs := make([]int64, 2)
	for i := range cardIDs {
		resp = ts.do(http.MethodPost, "/api/web/cards", map[string]any{
			"topic_id": topic.ID,
			"front":    fmt.Sprintf("front %d", i),
			"back":     "back",
		}, map[string]string{"X-CSRF-Token": csrf})
		var card struct {
			ID int64 `json:"id"`
		}
		decodeBody(t, resp, &card)
		cardIDs[i] = card.ID
	}

	// 预览: 不执行修改.
	resp = ts.do(http.MethodPost, "/api/web/topics/trash-preview", map[string]any{
		"ids": []int64{topic.ID},
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview status = %d", resp.StatusCode)
	}
	var preview struct {
		Topics         int64 `json:"topics"`
		AffectedCards  int64 `json:"affected_cards"`
		AlreadyTrashed int64 `json:"already_trashed"`
	}
	decodeBody(t, resp, &preview)
	if preview.Topics != 1 || preview.AffectedCards != 2 || preview.AlreadyTrashed != 0 {
		t.Fatalf("preview = %+v", preview)
	}

	// include_cards=false: 卡解除关联, version 加一, 仍是正常卡.
	resp = ts.do(http.MethodPost, fmt.Sprintf("/api/web/topics/%d/trash", topic.ID), map[string]any{
		"expected_version": 1, "include_cards": false,
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("trash status = %d", resp.StatusCode)
	}
	var trashResult struct {
		TrashedTopicID int64 `json:"trashed_topic_id"`
		AffectedCards  int64 `json:"affected_cards"`
	}
	decodeBody(t, resp, &trashResult)
	if trashResult.AffectedCards != 2 {
		t.Fatalf("affected_cards = %d, want 2", trashResult.AffectedCards)
	}
	resp = ts.do(http.MethodGet, fmt.Sprintf("/api/web/cards/%d", cardIDs[0]), nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detached card status = %d, want 200", resp.StatusCode)
	}
	var detached struct {
		TopicID *int64 `json:"topic_id"`
		Version int64  `json:"version"`
	}
	decodeBody(t, resp, &detached)
	if detached.TopicID != nil {
		t.Fatalf("detached card topic_id = %v, want null", detached.TopicID)
	}
	if detached.Version != 2 {
		t.Fatalf("detached card version = %d, want 2", detached.Version)
	}

	// 恢复 Topic, 再用 include_cards=true 回收: 卡连带进回收站.
	resp = ts.do(http.MethodPost, fmt.Sprintf("/api/web/trash/topics/%d/restore", topic.ID), map[string]any{
		"expected_version": 2,
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("restore topic status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	// 重新挂回 Topic.
	resp = ts.do(http.MethodPatch, fmt.Sprintf("/api/web/cards/%d", cardIDs[0]), map[string]any{
		"expected_version": 2, "topic_id": topic.ID,
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("relink card status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = ts.do(http.MethodPatch, fmt.Sprintf("/api/web/cards/%d", cardIDs[1]), map[string]any{
		"expected_version": 2, "topic_id": topic.ID,
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("relink card status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = ts.do(http.MethodPost, fmt.Sprintf("/api/web/topics/%d/trash", topic.ID), map[string]any{
		"expected_version": 3, "include_cards": true,
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("trash with cards status = %d", resp.StatusCode)
	}
	decodeBody(t, resp, &trashResult)
	if trashResult.AffectedCards != 2 {
		t.Fatalf("affected_cards = %d, want 2", trashResult.AffectedCards)
	}
	resp = ts.do(http.MethodGet, fmt.Sprintf("/api/web/cards/%d", cardIDs[0]), nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("card after include_cards trash status = %d, want 404", resp.StatusCode)
	}
	resp.Body.Close()
	resp = ts.do(http.MethodGet, "/api/web/trash/cards", nil, nil)
	var cardTrash struct {
		Total int64 `json:"total"`
	}
	decodeBody(t, resp, &cardTrash)
	if cardTrash.Total != 2 {
		t.Fatalf("trashed cards = %d, want 2", cardTrash.Total)
	}
}

// TestBatchOperations 验证批量创建/回收/恢复与失败 index.
func TestBatchOperations(t *testing.T) {
	ts := newTestServer(t)
	csrf := ts.login()

	// 批内同名冲突: 整批回滚, 错误携带 index.
	resp := ts.do(http.MethodPost, "/api/web/topics/batch-create", map[string]any{
		"items": []map[string]any{
			{"name": "A"},
			{"name": "B"},
			{"name": "A"},
		},
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("batch duplicate status = %d, want 409", resp.StatusCode)
	}
	var batchErr struct {
		Error struct {
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	decodeBody(t, resp, &batchErr)
	if idx, ok := batchErr.Error.Details["index"].(float64); !ok || int(idx) != 2 {
		t.Fatalf("batch error details = %+v", batchErr.Error.Details)
	}
	resp = ts.do(http.MethodGet, "/api/web/topics", nil, nil)
	var list struct {
		Total int64 `json:"total"`
	}
	decodeBody(t, resp, &list)
	if list.Total != 0 {
		t.Fatalf("topics after rollback = %d, want 0", list.Total)
	}

	// 合法批量创建.
	resp = ts.do(http.MethodPost, "/api/web/topics/batch-create", map[string]any{
		"items": []map[string]any{{"name": "A"}, {"name": "B"}},
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("batch create status = %d, want 201", resp.StatusCode)
	}
	var batch struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	decodeBody(t, resp, &batch)
	if len(batch.Items) != 2 {
		t.Fatalf("batch items = %d", len(batch.Items))
	}

	// 批量建卡 + 批量回收 + 批量恢复.
	var cards struct {
		Items []struct {
			ID      int64 `json:"id"`
			Version int64 `json:"version"`
		} `json:"items"`
	}
	resp = ts.do(http.MethodPost, "/api/web/cards/batch-create", map[string]any{
		"items": []map[string]any{
			{"front": "f1", "back": "b1"},
			{"front": "f2", "back": "b2"},
		},
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("batch create cards status = %d", resp.StatusCode)
	}
	decodeBody(t, resp, &cards)

	trashItems := make([]map[string]any, 0, len(cards.Items))
	for _, c := range cards.Items {
		trashItems = append(trashItems, map[string]any{
			"id": c.ID, "expected_version": c.Version,
		})
	}
	resp = ts.do(http.MethodPost, "/api/web/cards/batch-trash", map[string]any{
		"items": trashItems,
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("batch trash status = %d", resp.StatusCode)
	}
	var trashResult struct {
		TrashedCards int64 `json:"trashed_cards"`
	}
	decodeBody(t, resp, &trashResult)
	if trashResult.TrashedCards != 2 {
		t.Fatalf("trashed_cards = %d", trashResult.TrashedCards)
	}

	restoreItems := make([]map[string]any, 0, len(cards.Items))
	for i, c := range cards.Items {
		restoreItems = append(restoreItems, map[string]any{
			"id": c.ID, "expected_version": 2, // 原 version+1
		})
		_ = i
	}
	resp = ts.do(http.MethodPost, "/api/web/trash/cards/batch-restore", map[string]any{
		"items": restoreItems,
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("batch restore status = %d", resp.StatusCode)
	}
	var restoreResult struct {
		Restored []struct {
			TrashCardID int64 `json:"trash_card_id"`
			NewCardID   int64 `json:"new_card_id"`
		} `json:"restored"`
	}
	decodeBody(t, resp, &restoreResult)
	if len(restoreResult.Restored) != 2 {
		t.Fatalf("restored = %d", len(restoreResult.Restored))
	}
	for _, r := range restoreResult.Restored {
		if r.NewCardID == r.TrashCardID {
			t.Fatalf("restore should generate new id: %+v", r)
		}
	}
}

// TestCardListFilters 验证列表筛选与排序参数.
func TestCardListFilters(t *testing.T) {
	ts := newTestServer(t)
	csrf := ts.login()

	resp := ts.do(http.MethodPost, "/api/web/topics", map[string]any{"name": "F"},
		map[string]string{"X-CSRF-Token": csrf})
	var topic struct {
		ID int64 `json:"id"`
	}
	decodeBody(t, resp, &topic)

	// 三张卡: 有 Topic 无 embedding / 无 Topic 有 embedding / 无 Topic 无 embedding.
	resp = ts.do(http.MethodPost, "/api/web/cards/batch-create", map[string]any{
		"items": []map[string]any{
			{"topic_id": topic.ID, "front": "Alpha", "back": "x", "enable_embedding": false},
			{"front": "beta search", "back": "y", "enable_embedding": true},
			{"front": "Gamma", "back": "z search", "enable_embedding": true},
		},
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("batch create status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	list := func(query string) (items []struct {
		ID              int64  `json:"id"`
		TopicID         *int64 `json:"topic_id"`
		EmbeddingStatus string `json:"embedding_status"`
	}, total int64) {
		t.Helper()
		resp := ts.do(http.MethodGet, "/api/web/cards"+query, nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("list %s status = %d", query, resp.StatusCode)
		}
		var out struct {
			Items []struct {
				ID              int64  `json:"id"`
				TopicID         *int64 `json:"topic_id"`
				EmbeddingStatus string `json:"embedding_status"`
			} `json:"items"`
			Total int64 `json:"total"`
		}
		decodeBody(t, resp, &out)
		return out.Items, out.Total
	}

	// topic_id=0: 只有无 Topic 的卡.
	items, total := list("?topic_id=0")
	if total != 2 {
		t.Fatalf("no-topic cards = %d, want 2", total)
	}
	for _, item := range items {
		if item.TopicID != nil {
			t.Fatalf("expected no topic, got %v", item.TopicID)
		}
	}

	// topic_id 指定: 只有该 Topic 的卡.
	items, total = list(fmt.Sprintf("?topic_id=%d", topic.ID))
	if total != 1 || items[0].EmbeddingStatus != "disabled" {
		t.Fatalf("topic cards = %d %+v", total, items)
	}

	// q 子串搜索 (ASCII 大小写不敏感).
	_, total = list("?q=SEARCH")
	if total != 2 {
		t.Fatalf("q search = %d, want 2", total)
	}

	// embedding_status 筛选.
	_, total = list("?embedding_status=pending")
	if total != 2 {
		t.Fatalf("pending cards = %d, want 2", total)
	}
	_, total = list("?embedding_status=disabled")
	if total != 1 {
		t.Fatalf("disabled cards = %d, want 1", total)
	}

	// 非法参数报校验错误.
	resp = ts.do(http.MethodGet, "/api/web/cards?sort=bogus", nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bogus sort status = %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestCardEmbeddingStatusTransitions 验证修改卡时 embedding 状态迁移.
func TestCardEmbeddingStatusTransitions(t *testing.T) {
	ts := newTestServer(t)
	csrf := ts.login()

	// enable_embedding=false: disabled.
	resp := ts.do(http.MethodPost, "/api/web/cards", map[string]any{
		"front": "front", "back": "back", "enable_embedding": false,
	}, map[string]string{"X-CSRF-Token": csrf})
	var card struct {
		ID              int64  `json:"id"`
		Version         int64  `json:"version"`
		EmbeddingStatus string `json:"embedding_status"`
	}
	decodeBody(t, resp, &card)
	if card.EmbeddingStatus != "disabled" {
		t.Fatalf("status = %s, want disabled", card.EmbeddingStatus)
	}

	// false -> true: pending.
	resp = ts.do(http.MethodPatch, fmt.Sprintf("/api/web/cards/%d", card.ID), map[string]any{
		"expected_version": card.Version, "enable_embedding": true,
	}, map[string]string{"X-CSRF-Token": csrf})
	decodeBody(t, resp, &card)
	if card.EmbeddingStatus != "pending" || card.Version != 2 {
		t.Fatalf("after enable = %+v", card)
	}

	// front 变化: 仍 pending, version 递增.
	resp = ts.do(http.MethodPatch, fmt.Sprintf("/api/web/cards/%d", card.ID), map[string]any{
		"expected_version": card.Version, "front": "new front",
	}, map[string]string{"X-CSRF-Token": csrf})
	decodeBody(t, resp, &card)
	if card.EmbeddingStatus != "pending" || card.Version != 3 {
		t.Fatalf("after front change = %+v", card)
	}

	// true -> false: disabled.
	resp = ts.do(http.MethodPatch, fmt.Sprintf("/api/web/cards/%d", card.ID), map[string]any{
		"expected_version": card.Version, "enable_embedding": false,
	}, map[string]string{"X-CSRF-Token": csrf})
	decodeBody(t, resp, &card)
	if card.EmbeddingStatus != "disabled" {
		t.Fatalf("after disable = %+v", card)
	}

	// topic_id 三态: 显式 null 设为无 Topic.
	resp = ts.do(http.MethodPatch, fmt.Sprintf("/api/web/cards/%d", card.ID), map[string]any{
		"expected_version": card.Version, "topic_id": nil,
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set null topic status = %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestGlossaryFlow 验证 Glossary 全流程与同名冲突.
func TestGlossaryFlow(t *testing.T) {
	ts := newTestServer(t)
	csrf := ts.login()

	resp := ts.do(http.MethodPost, "/api/web/glossary", map[string]any{
		"term": "closure", "definition": "闭包是捕获了外部变量的函数.",
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create glossary status = %d", resp.StatusCode)
	}
	var g struct {
		ID      int64 `json:"id"`
		Version int64 `json:"version"`
	}
	decodeBody(t, resp, &g)

	// 同名冲突.
	resp = ts.do(http.MethodPost, "/api/web/glossary", map[string]any{
		"term": "closure", "definition": "重复",
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate glossary status = %d, want 409", resp.StatusCode)
	}
	resp.Body.Close()

	// 修改与回收.
	resp = ts.do(http.MethodPatch, fmt.Sprintf("/api/web/glossary/%d", g.ID), map[string]any{
		"expected_version": g.Version, "definition": "新定义",
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update glossary status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = ts.do(http.MethodPost, fmt.Sprintf("/api/web/glossary/%d/trash", g.ID), map[string]any{
		"expected_version": 2,
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("trash glossary status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 同名重建覆盖回收站记录: 新建同名 Glossary 时旧回收站行被物理删除,
	// 因此恢复旧 ID 返回 404 而不是 name_conflict -- 同名冲突在
	// "创建/改名必覆盖回收站"的规则下无法经由 API 构造,
	// 服务层的冲突检查属于纵深防御.
	resp = ts.do(http.MethodPost, "/api/web/glossary", map[string]any{
		"term": "closure", "definition": "占名",
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("recreate glossary status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = ts.do(http.MethodPost, fmt.Sprintf("/api/web/trash/glossary/%d/restore", g.ID), map[string]any{
		"expected_version": 3,
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("restore overwritten glossary status = %d, want 404", resp.StatusCode)
	}
	resp.Body.Close()
	resp = ts.do(http.MethodGet, "/api/web/trash/glossary", nil, nil)
	var glossaryTrash struct {
		Total int64 `json:"total"`
	}
	decodeBody(t, resp, &glossaryTrash)
	if glossaryTrash.Total != 0 {
		t.Fatalf("trashed glossary after overwrite = %d, want 0", glossaryTrash.Total)
	}
}

// TestEmptyTrash 验证清空回收站返回各类删除数量.
func TestEmptyTrash(t *testing.T) {
	ts := newTestServer(t)
	csrf := ts.login()

	// 准备: 一个 Topic (连带一张卡) + 一条 Glossary, 全部回收.
	resp := ts.do(http.MethodPost, "/api/web/topics", map[string]any{"name": "E"},
		map[string]string{"X-CSRF-Token": csrf})
	var topic struct {
		ID int64 `json:"id"`
	}
	decodeBody(t, resp, &topic)
	resp = ts.do(http.MethodPost, "/api/web/cards", map[string]any{
		"topic_id": topic.ID, "front": "f", "back": "b",
	}, map[string]string{"X-CSRF-Token": csrf})
	resp.Body.Close()
	resp = ts.do(http.MethodPost, "/api/web/glossary", map[string]any{
		"term": "t", "definition": "d",
	}, map[string]string{"X-CSRF-Token": csrf})
	resp.Body.Close()

	resp = ts.do(http.MethodPost, fmt.Sprintf("/api/web/topics/%d/trash", topic.ID), map[string]any{
		"expected_version": 1, "include_cards": true,
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("trash topic status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = ts.do(http.MethodPost, "/api/web/glossary/1/trash", map[string]any{
		"expected_version": 1,
	}, map[string]string{"X-CSRF-Token": csrf})
	resp.Body.Close()

	// 清空.
	resp = ts.do(http.MethodPost, "/api/web/trash/empty", nil, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("empty trash status = %d, want 200", resp.StatusCode)
	}
	var result struct {
		Cards    int64 `json:"cards"`
		Topics   int64 `json:"topics"`
		Glossary int64 `json:"glossary"`
	}
	decodeBody(t, resp, &result)
	if result.Cards != 1 || result.Topics != 1 || result.Glossary != 1 {
		t.Fatalf("empty result = %+v", result)
	}
	for _, path := range []string{"/api/web/trash/cards", "/api/web/trash/topics", "/api/web/trash/glossary"} {
		resp = ts.do(http.MethodGet, path, nil, nil)
		var out struct {
			Total int64 `json:"total"`
		}
		decodeBody(t, resp, &out)
		if out.Total != 0 {
			t.Fatalf("%s total = %d, want 0", path, out.Total)
		}
	}
}

// TestBatchLimitExceeded 验证批量上限 100.
func TestBatchLimitExceeded(t *testing.T) {
	ts := newTestServer(t)
	csrf := ts.login()

	items := make([]map[string]any, 101)
	for i := range items {
		items[i] = map[string]any{"name": fmt.Sprintf("T%d", i)}
	}
	resp := ts.do(http.MethodPost, "/api/web/topics/batch-create", map[string]any{
		"items": items,
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("batch limit status = %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()
}
