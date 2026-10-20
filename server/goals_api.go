package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/Autumn-27/artex/db"
)

// goals_api.go 가 사용자에게 돌려주는 오류 응답 문구. 명령·요청 필드명(text·vulnclass·
// gid)·enum 은 원문 그대로 두고, 사람이 읽는 메시지만 한국어로 둔다. 영어 writeErr
// (task not found·invalid JSON·bad goal id)는 F3b 중국어 전용 범위라 보존한다.
const (
	errGoalTaskDeletingAdd    = "작업을 삭제하는 중이라 목표를 추가할 수 없습니다"
	errGoalTaskDeletingEdit   = "작업을 삭제하는 중이라 목표를 수정할 수 없습니다"
	errGoalTaskDeletingDelete = "작업을 삭제하는 중이라 목표를 삭제할 수 없습니다"
	errGoalTextEmpty          = "목표 내용은 비워 둘 수 없습니다"
	errGoalNotFound           = "목표를 찾을 수 없습니다"
	errGoalReadAfterAdd       = "목표를 저장한 뒤 읽지 못했습니다"
	errGoalReadAfterEdit      = "목표를 수정한 뒤 읽지 못했습니다"
)

// Human CRUD endpoints for the overview's Goal Management UI. These edit the same
// goal nodes as the agent's set_goals tool. Adding or editing a goal resumes the task
// through admitTask (terminal state to running, unpause, and queue if needed).
// Deletion does not revive the task by product design. Each mutation uses
// beginTaskOperation/decInflight to avoid racing with task deletion, as intent CRUD does.

// listGoals returns every goal in this task with text, vulnclass, and state unpacked.
func (s *Server) listGoals(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "task not found")
		return
	}
	goals, err := t.Store.ListByKind(db.KindGoal, 10000)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"goals": goalDTOs(goals)})
}

// addGoal saves a human-created goal under the task root's spawns edge, records
// a goal-added trigger, wakes the planner, and revives the task for reassessment.
func (s *Server) addGoal(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "task not found")
		return
	}
	if !s.engine.beginTaskOperation(t.ID) {
		writeErr(w, 409, errGoalTaskDeletingAdd)
		return
	}
	defer s.engine.decInflight(t.ID)

	var body struct {
		Text      string `json:"text"`
		VulnClass string `json:"vulnclass"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid JSON")
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" {
		writeErr(w, 400, errGoalTextEmpty)
		return
	}
	payload := map[string]any{"text": text}
	if vc := strings.TrimSpace(body.VulnClass); vc != "" {
		payload["vulnclass"] = vc
	}
	id, err := t.Store.AddGoal(payload, "human")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if of, _ := t.Store.OriginFactID(); of > 0 && id > 0 {
		_ = t.Store.Link(of, db.RelSpawns, id) // goal descends from the task root (origin fact)
	}
	t.NotifyGoal([]string{text}) // record the new goal and wake the planner
	s.reviveTask(t)              // resume a completed or paused task
	node, _ := t.Store.GetNode(id)
	if node == nil {
		writeErr(w, 500, errGoalReadAfterAdd)
		return
	}
	writeJSON(w, 200, goalDTO(node))
}

// editGoal updates a goal's text and vulnclass, records the old-to-new change,
// wakes the planner, and revives the task to adjust its direction.
func (s *Server) editGoal(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "task not found")
		return
	}
	if !s.engine.beginTaskOperation(t.ID) {
		writeErr(w, 409, errGoalTaskDeletingEdit)
		return
	}
	defer s.engine.decInflight(t.ID)

	gid, err := strconv.ParseInt(r.PathValue("gid"), 10, 64)
	if err != nil || gid <= 0 {
		writeErr(w, 400, "bad goal id")
		return
	}
	var body struct {
		Text      string `json:"text"`
		VulnClass string `json:"vulnclass"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid JSON")
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" {
		writeErr(w, 400, errGoalTextEmpty)
		return
	}
	node, err := t.Store.GetNode(gid)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if node == nil || node.Kind != db.KindGoal {
		writeErr(w, 404, errGoalNotFound)
		return
	}
	oldText := goalDTO(node).Text
	if err := t.Store.UpdateGoalPayload(gid, text, strings.TrimSpace(body.VulnClass)); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	t.NotifyGoalEdited(oldText, text) // record the change and wake the planner
	s.reviveTask(t)                   // reassess the task against the edited goal
	updated, _ := t.Store.GetNode(gid)
	if updated == nil {
		writeErr(w, 500, errGoalReadAfterEdit)
		return
	}
	writeJSON(w, 200, goalDTO(updated))
}

// deleteGoal permanently removes a human-selected goal and its edges/anchors,
// then records a deletion trigger so the planner can reassess remaining goals.
// By product design, deletion does not revive the task.
func (s *Server) deleteGoal(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "task not found")
		return
	}
	if !s.engine.beginTaskOperation(t.ID) {
		writeErr(w, 409, errGoalTaskDeletingDelete)
		return
	}
	defer s.engine.decInflight(t.ID)

	gid, err := strconv.ParseInt(r.PathValue("gid"), 10, 64)
	if err != nil || gid <= 0 {
		writeErr(w, 400, "bad goal id")
		return
	}
	node, err := t.Store.GetNode(gid)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if node == nil || node.Kind != db.KindGoal {
		writeErr(w, 404, errGoalNotFound)
		return
	}
	text := goalDTO(node).Text
	if err := t.Store.DeleteGoal(gid); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	t.NotifyGoalDeleted(text) // record the deletion and wake the planner without reviving the task
	writeJSON(w, 200, map[string]bool{"ok": true})
}
