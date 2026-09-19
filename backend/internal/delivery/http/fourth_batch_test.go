package httpapi_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// calendarWindow 返回覆盖今天的一小段闭区间查询串, 供测试读取日历计数.
// 跨度远小于 366 天上限, 避免撞上日历参数校验.
func calendarWindow() string {
	today := time.Now().UTC()
	from := today.AddDate(0, 0, -3).Format("2006-01-02")
	to := today.AddDate(0, 0, 3).Format("2006-01-02")
	return fmt.Sprintf("/api/web/calendar?from=%s&to=%s", from, to)
}

// cardDetailLite 是第四批测试用到的 Card 详情子集.
type cardDetailLite struct {
	ID       int64  `json:"id"`
	TopicID  *int64 `json:"topic_id"`
	Front    string `json:"front"`
	Back     string `json:"back"`
	Version  int64  `json:"version"`
	Schedule *struct {
		Due           string `json:"due"`
		State         string `json:"state"`
		Version       int64  `json:"version"`
		Reps          int64  `json:"reps"`
		Lapses        int64  `json:"lapses"`
		ScheduledDays int64  `json:"scheduled_days"`
	} `json:"schedule"`
}

// createCardWeb 建一张卡并返回详情.
func createCardWeb(t *testing.T, ts *testServer, csrf string, topicID *int64, front, back string) cardDetailLite {
	t.Helper()
	body := map[string]any{"front": front, "back": back}
	if topicID != nil {
		body["topic_id"] = *topicID
	}
	resp := ts.do(http.MethodPost, "/api/web/cards", body,
		map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create card status = %d, want 201", resp.StatusCode)
	}
	var card cardDetailLite
	decodeBody(t, resp, &card)
	return card
}

// createTopicWeb 建一个 Topic 并返回 ID.
func createTopicWeb(t *testing.T, ts *testServer, csrf, name string) int64 {
	t.Helper()
	resp := ts.do(http.MethodPost, "/api/web/topics",
		map[string]any{"name": name, "description": ""},
		map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create topic status = %d, want 201", resp.StatusCode)
	}
	var topic struct {
		ID int64 `json:"id"`
	}
	decodeBody(t, resp, &topic)
	return topic.ID
}

// TestFourthBatchAcceptance 覆盖第四批验收:
// 建卡 → 复习评分 → 校验 schedule 变化 + review_log 写入 + calendar 计数,
// 以及复习历史端点与版本冲突.
func TestFourthBatchAcceptance(t *testing.T) {
	ts := newTestServer(t)
	csrf := ts.login()
	topicID := createTopicWeb(t, ts, csrf, "第四批")

	card := createCardWeb(t, ts, csrf, &topicID, "Go slice 的底层结构?", "slice header")
	if card.TopicID == nil || *card.TopicID != topicID {
		t.Fatalf("card topic = %v", card.TopicID)
	}

	// 1. 到期队列包含刚建的新卡 (due=now).
	resp := ts.do(http.MethodGet, "/api/web/review/due", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("review due status = %d, want 200", resp.StatusCode)
	}
	var due struct {
		Items []struct {
			CardID          int64  `json:"card_id"`
			CardVersion     int64  `json:"card_version"`
			ScheduleVersion int64  `json:"schedule_version"`
			State           string `json:"state"`
		} `json:"items"`
		Total int64 `json:"total"`
	}
	decodeBody(t, resp, &due)
	if due.Total != 1 || len(due.Items) != 1 || due.Items[0].CardID != card.ID {
		t.Fatalf("due = %+v", due)
	}
	if due.Items[0].State != "new" {
		t.Fatalf("due state = %s, want new", due.Items[0].State)
	}

	// 2. 提交复习 good: schedule 版本 +1, 状态变 review, 返回 review_log_id.
	resp = ts.do(http.MethodPost, fmt.Sprintf("/api/web/review/%d/submit", card.ID), map[string]any{
		"rating":                    "good",
		"expected_card_version":     card.Version,
		"expected_schedule_version": card.Schedule.Version,
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("review submit status = %d, want 200", resp.StatusCode)
	}
	var submitted struct {
		CardID      int64  `json:"card_id"`
		Rating      string `json:"rating"`
		ReviewLogID int64  `json:"review_log_id"`
		Schedule    struct {
			State   string `json:"state"`
			Version int64  `json:"version"`
			Reps    int64  `json:"reps"`
		} `json:"schedule"`
	}
	decodeBody(t, resp, &submitted)
	if submitted.CardID != card.ID || submitted.Rating != "good" {
		t.Fatalf("submit result = %+v", submitted)
	}
	if submitted.ReviewLogID == 0 {
		t.Fatal("review_log_id is zero")
	}
	if submitted.Schedule.State != "review" {
		t.Fatalf("schedule state = %s, want review", submitted.Schedule.State)
	}
	if submitted.Schedule.Version != card.Schedule.Version+1 {
		t.Fatalf("schedule version = %d, want %d", submitted.Schedule.Version, card.Schedule.Version+1)
	}
	if submitted.Schedule.Reps != 1 {
		t.Fatalf("schedule reps = %d, want 1", submitted.Schedule.Reps)
	}

	// 3. 复习历史端点返回刚才那一条, reviewed_at 降序.
	resp = ts.do(http.MethodGet, fmt.Sprintf("/api/web/cards/%d/reviews", card.ID), nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("card reviews status = %d, want 200", resp.StatusCode)
	}
	var reviews struct {
		Items []struct {
			ID     int64  `json:"id"`
			Rating string `json:"rating"`
			State  string `json:"state"`
		} `json:"items"`
		Total int64 `json:"total"`
	}
	decodeBody(t, resp, &reviews)
	if reviews.Total != 1 || len(reviews.Items) != 1 {
		t.Fatalf("reviews = %+v", reviews)
	}
	if reviews.Items[0].ID != submitted.ReviewLogID || reviews.Items[0].Rating != "good" {
		t.Fatalf("review item = %+v", reviews.Items[0])
	}

	// 4. 日历计数: 今天 created_cards 含本卡, review_events=1.
	today := ts.do(http.MethodGet, calendarWindow(), nil, nil)
	if today.StatusCode != http.StatusOK {
		t.Fatalf("calendar status = %d, want 200", today.StatusCode)
	}
	var cal struct {
		Days []struct {
			Date         string `json:"date"`
			CreatedCards int64  `json:"created_cards"`
			ReviewEvents int64  `json:"review_events"`
		} `json:"days"`
	}
	decodeBody(t, today, &cal)
	var cardTotal, reviewTotal int64
	for _, d := range cal.Days {
		cardTotal += d.CreatedCards
		reviewTotal += d.ReviewEvents
	}
	if cardTotal != 1 {
		t.Fatalf("calendar created_cards total = %d, want 1", cardTotal)
	}
	if reviewTotal != 1 {
		t.Fatalf("calendar review_events total = %d, want 1", reviewTotal)
	}

	// 5. 用过期 schedule 版本再提交: 409, details 带两个当前版本.
	resp = ts.do(http.MethodPost, fmt.Sprintf("/api/web/review/%d/submit", card.ID), map[string]any{
		"rating":                    "again",
		"expected_card_version":     card.Version,
		"expected_schedule_version": card.Schedule.Version,
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("stale review status = %d, want 409", resp.StatusCode)
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
	if _, ok := conflict.Error.Details["current_card_version"]; !ok {
		t.Fatalf("details missing current_card_version: %+v", conflict.Error.Details)
	}
	if _, ok := conflict.Error.Details["current_schedule_version"]; !ok {
		t.Fatalf("details missing current_schedule_version: %+v", conflict.Error.Details)
	}

	// 6. 非法评分返回 400.
	resp = ts.do(http.MethodPost, fmt.Sprintf("/api/web/review/%d/submit", card.ID), map[string]any{
		"rating":                    "manual",
		"expected_card_version":     card.Version,
		"expected_schedule_version": card.Schedule.Version + 1,
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid rating status = %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestFourthBatchReviewConflictDoesNotWrite 验证版本冲突时事务整体回滚,
// 既不更新调度也不写入 review_log.
func TestFourthBatchReviewConflictDoesNotWrite(t *testing.T) {
	ts := newTestServer(t)
	csrf := ts.login()
	card := createCardWeb(t, ts, csrf, nil, "冲突测试 front", "冲突测试 back")

	// 用错误的 card version 提交, 冲突.
	resp := ts.do(http.MethodPost, fmt.Sprintf("/api/web/review/%d/submit", card.ID), map[string]any{
		"rating":                    "good",
		"expected_card_version":     card.Version + 99,
		"expected_schedule_version": card.Schedule.Version,
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("conflict status = %d, want 409", resp.StatusCode)
	}
	resp.Body.Close()

	// 复习历史应仍为空.
	resp = ts.do(http.MethodGet, fmt.Sprintf("/api/web/cards/%d/reviews", card.ID), nil, nil)
	var reviews struct {
		Total int64 `json:"total"`
	}
	decodeBody(t, resp, &reviews)
	if reviews.Total != 0 {
		t.Fatalf("reviews after conflict = %d, want 0", reviews.Total)
	}
}

// TestFourthBatchCardMerge 覆盖合并: 继承 Topic、来源卡进回收站、
// 新卡 created_cards +1 且 Q1/Q2 拼接.
func TestFourthBatchCardMerge(t *testing.T) {
	ts := newTestServer(t)
	csrf := ts.login()
	topicID := createTopicWeb(t, ts, csrf, "合并主题")

	first := createCardWeb(t, ts, csrf, &topicID, "问题一", "答案一")
	second := createCardWeb(t, ts, csrf, &topicID, "问题二", "答案二")

	resp := ts.do(http.MethodPost, "/api/web/cards/merge", map[string]any{
		"source_card_ids": []int64{first.ID, second.ID},
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("merge status = %d, want 201", resp.StatusCode)
	}
	var merged cardDetailLite
	decodeBody(t, resp, &merged)
	if merged.TopicID == nil || *merged.TopicID != topicID {
		t.Fatalf("merged topic = %v, want %d", merged.TopicID, topicID)
	}
	wantFront := "Q1: 问题一\nQ2: 问题二"
	wantBack := "Q1: 答案一\nQ2: 答案二"
	if merged.Front != wantFront || merged.Back != wantBack {
		t.Fatalf("merged content = %q / %q, want %q / %q", merged.Front, merged.Back, wantFront, wantBack)
	}
	if merged.Schedule == nil || merged.Schedule.State != "new" {
		t.Fatalf("merged schedule = %+v", merged.Schedule)
	}

	// 来源卡应 404 (已进回收站), 回收站里能看到两张.
	resp = ts.do(http.MethodGet, fmt.Sprintf("/api/web/cards/%d", first.ID), nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("source card after merge = %d, want 404", resp.StatusCode)
	}
	resp.Body.Close()
	resp = ts.do(http.MethodGet, "/api/web/trash/cards", nil, nil)
	var trash struct {
		Total int64 `json:"total"`
	}
	decodeBody(t, resp, &trash)
	if trash.Total != 2 {
		t.Fatalf("trash total = %d, want 2", trash.Total)
	}

	// 日历 created_cards: 两张来源卡 + 合并新卡 = 3.
	resp = ts.do(http.MethodGet, calendarWindow(), nil, nil)
	var cal struct {
		Days []struct {
			CreatedCards int64 `json:"created_cards"`
		} `json:"days"`
	}
	decodeBody(t, resp, &cal)
	var total int64
	for _, d := range cal.Days {
		total += d.CreatedCards
	}
	if total != 3 {
		t.Fatalf("calendar created_cards = %d, want 3", total)
	}
}

// TestFourthBatchMergeTopicRequired 验证来源卡 Topic 不同且未显式指定时报 409.
func TestFourthBatchMergeTopicRequired(t *testing.T) {
	ts := newTestServer(t)
	csrf := ts.login()
	topicA := createTopicWeb(t, ts, csrf, "主题A")
	topicB := createTopicWeb(t, ts, csrf, "主题B")

	first := createCardWeb(t, ts, csrf, &topicA, "A front", "A back")
	second := createCardWeb(t, ts, csrf, &topicB, "B front", "B back")

	resp := ts.do(http.MethodPost, "/api/web/cards/merge", map[string]any{
		"source_card_ids": []int64{first.ID, second.ID},
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("merge topic conflict status = %d, want 409", resp.StatusCode)
	}
	var out struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decodeBody(t, resp, &out)
	if out.Error.Code != "merge_topic_required" {
		t.Fatalf("code = %s, want merge_topic_required", out.Error.Code)
	}

	// 显式传 null 表示无 Topic, 可以合并.
	resp = ts.do(http.MethodPost, "/api/web/cards/merge", map[string]any{
		"source_card_ids": []int64{first.ID, second.ID},
		"topic_id":        nil,
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("merge with null topic status = %d, want 201", resp.StatusCode)
	}
	var merged cardDetailLite
	decodeBody(t, resp, &merged)
	if merged.TopicID != nil {
		t.Fatalf("merged topic = %v, want nil", merged.TopicID)
	}
}

// TestFourthBatchCLIMergeApproval 验证 CLI 合并提案 → Web 批准 → 合并生效,
// 且来源卡版本变化会让提案 stale.
func TestFourthBatchCLIMergeApproval(t *testing.T) {
	ts := newTestServer(t)
	cli := ts.newCLIClient(t)
	csrf := ts.login()
	topicID := createTopicWeb(t, ts, csrf, "CLI 合并")

	first := createCardWeb(t, ts, csrf, &topicID, "CLI 一", "答案一")
	second := createCardWeb(t, ts, csrf, &topicID, "CLI 二", "答案二")

	// CLI 提交合并提案, 期望 202.
	resp := cli.do(http.MethodPost, "/api/cli/cards/merge", map[string]any{
		"source_card_ids": []int64{first.ID, second.ID},
	}, "merge-key-1")
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("cli merge propose status = %d, want 202", resp.StatusCode)
	}
	var proposed struct {
		Approval approvalSummary `json:"approval"`
	}
	decodeBody(t, resp, &proposed)

	// 详情应含两个 source target.
	resp = ts.do(http.MethodGet, fmt.Sprintf("/api/web/approvals/%d", proposed.Approval.ID), nil, nil)
	var detail approvalDetail
	decodeBody(t, resp, &detail)
	if detail.Operation != "card_merge" {
		t.Fatalf("operation = %s, want card_merge", detail.Operation)
	}
	roles := map[string]bool{}
	for _, tg := range detail.Targets {
		roles[tg.Role] = true
	}
	if !roles["source_1"] || !roles["source_2"] {
		t.Fatalf("merge targets roles = %+v", detail.Targets)
	}

	// Web 批准.
	resp = ts.do(http.MethodPost, fmt.Sprintf("/api/web/approvals/%d/approve", proposed.Approval.ID), map[string]any{},
		map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("approve status = %d, want 200", resp.StatusCode)
	}
	var approved approvalDetail
	decodeBody(t, resp, &approved)
	if approved.Status != "approved" {
		t.Fatalf("approved status = %s", approved.Status)
	}

	// 来源卡已进回收站.
	resp = ts.do(http.MethodGet, fmt.Sprintf("/api/web/cards/%d", first.ID), nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("source1 after approve = %d, want 404", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestFourthBatchMergeApprovalStale 验证来源卡在提案后变化会让请求 stale.
func TestFourthBatchMergeApprovalStale(t *testing.T) {
	ts := newTestServer(t)
	cli := ts.newCLIClient(t)
	csrf := ts.login()
	topicID := createTopicWeb(t, ts, csrf, "stale 合并")

	first := createCardWeb(t, ts, csrf, &topicID, "stale 一", "答案一")
	second := createCardWeb(t, ts, csrf, &topicID, "stale 二", "答案二")

	resp := cli.do(http.MethodPost, "/api/cli/cards/merge", map[string]any{
		"source_card_ids": []int64{first.ID, second.ID},
	}, "merge-key-stale")
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("cli merge propose status = %d, want 202", resp.StatusCode)
	}
	var proposed struct {
		Approval approvalSummary `json:"approval"`
	}
	decodeBody(t, resp, &proposed)

	// 改动来源卡 second, 使其版本与提案快照不一致.
	resp = ts.do(http.MethodPatch, fmt.Sprintf("/api/web/cards/%d", second.ID), map[string]any{
		"expected_version": second.Version,
		"back":             "被改动",
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch second status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	// 批准应转 stale, 不执行业务修改.
	resp = ts.do(http.MethodPost, fmt.Sprintf("/api/web/approvals/%d/approve", proposed.Approval.ID), map[string]any{},
		map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("approve status = %d, want 200", resp.StatusCode)
	}
	var approved approvalDetail
	decodeBody(t, resp, &approved)
	if approved.Status != "stale" {
		t.Fatalf("status = %s, want stale", approved.Status)
	}
	if approved.Reason == nil || *approved.Reason != "target_version_changed" {
		t.Fatalf("reason = %v, want target_version_changed", approved.Reason)
	}

	// 两张来源卡仍在 (未被回收).
	for _, id := range []int64{first.ID, second.ID} {
		resp = ts.do(http.MethodGet, fmt.Sprintf("/api/web/cards/%d", id), nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("source %d status = %d, want 200", id, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

// TestFourthBatchCLIReviewDirect 验证 CLI 复习直接生效, 不经审批.
func TestFourthBatchCLIReviewDirect(t *testing.T) {
	ts := newTestServer(t)
	cli := ts.newCLIClient(t)
	csrf := ts.login()
	card := createCardWeb(t, ts, csrf, nil, "CLI 复习", "答案")

	// CLI 到期队列.
	resp := cli.do(http.MethodGet, "/api/cli/review/due", nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cli due status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	// CLI 直接提交复习, 应 200 (不是 202).
	resp = cli.do(http.MethodPost, fmt.Sprintf("/api/cli/review/%d/submit", card.ID), map[string]any{
		"rating":                    "easy",
		"expected_card_version":     card.Version,
		"expected_schedule_version": card.Schedule.Version,
	}, "cli-review-key")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cli review submit status = %d, want 200", resp.StatusCode)
	}
	var submitted struct {
		Rating string `json:"rating"`
	}
	decodeBody(t, resp, &submitted)
	if submitted.Rating != "easy" {
		t.Fatalf("rating = %s, want easy", submitted.Rating)
	}

	// 同 Key 重试: 返回首次结果, 不重复评分 (review_events 仍为 1).
	resp = cli.do(http.MethodPost, fmt.Sprintf("/api/cli/review/%d/submit", card.ID), map[string]any{
		"rating":                    "easy",
		"expected_card_version":     card.Version,
		"expected_schedule_version": card.Schedule.Version,
	}, "cli-review-key")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cli review replay status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	resp = ts.do(http.MethodGet, calendarWindow(), nil, nil)
	var cal struct {
		Days []struct {
			ReviewEvents int64 `json:"review_events"`
		} `json:"days"`
	}
	decodeBody(t, resp, &cal)
	var total int64
	for _, d := range cal.Days {
		total += d.ReviewEvents
	}
	if total != 1 {
		t.Fatalf("review_events = %d, want 1 (idempotent replay)", total)
	}
}

// TestFourthBatchCalendarValidation 验证 from/to 必填与跨度上限.
func TestFourthBatchCalendarValidation(t *testing.T) {
	ts := newTestServer(t)
	ts.login()

	resp := ts.do(http.MethodGet, "/api/web/calendar", nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing range status = %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()

	resp = ts.do(http.MethodGet, "/api/web/calendar?from=2026-01-01&to=2030-01-01", nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("oversized range status = %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()

	resp = ts.do(http.MethodGet, "/api/web/calendar?from=2026-01-02&to=2026-01-01", nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("reversed range status = %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()
}
