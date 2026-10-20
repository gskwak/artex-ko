package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/Autumn-27/artex/db"
)

// Human CRUD endpoints for constraint management and resolution of injection-scope switches.
// Operational constraints (allow/deny) share the task_constraints table with the agent-side
// set_constraints tool; these endpoints let users add, edit, and delete them directly in the UI.
// Constraints only provide prompt context. Changes do not notify the planner; its next planning
// round reads the database and picks them up (by product design). Each mutation handler calls
// beginTaskOperation/decInflight to avoid racing with task deletion, as goal/intent CRUD does.

// Settings keys for injection-scope switches, both enabled by default (GetBool's second argument is true).
const (
	settingConstraintsInjectPlanner = "constraints_inject_planner"
	settingConstraintsInjectWorker  = "constraints_inject_worker"
)

// Constraint CRUD 핸들러의 사용자 노출 에러 응답(한국어). writeErr 로 그대로 UI 토스트에 노출된다.
// Terminology: constraint→제약(ko.json 의 약속/제약 표기와 정합), task→작업. 추가/수정/삭제 문구는
// goals_api.go 의 errGoalTaskDeleting* 와 같은 문형이고, kind 검증은 intercept.go 의
// "값은 … 중 하나여야 합니다" 패턴을 따른다.
const (
	errConstraintTaskDeletingAdd    = "작업을 삭제하는 중이라 제약을 추가할 수 없습니다"
	errConstraintTaskDeletingEdit   = "작업을 삭제하는 중이라 제약을 수정할 수 없습니다"
	errConstraintTaskDeletingDelete = "작업을 삭제하는 중이라 제약을 삭제할 수 없습니다"
	errConstraintTextEmpty          = "제약 내용은 비워 둘 수 없습니다"
	errConstraintKindInvalid        = "kind 값은 allow 또는 deny 중 하나여야 합니다"
)

// constraintInjectPlanner / constraintInjectWorker report whether operational constraints are
// injected into the corresponding agent's system prompt (enabled by default). They are passed
// to the planner/worker as resolvers and read each round, so switch changes take effect immediately.
func (s *Server) constraintInjectPlanner() bool {
	return s.m.pg.GetBool(settingConstraintsInjectPlanner, true)
}

func (s *Server) constraintInjectWorker() bool {
	return s.m.pg.GetBool(settingConstraintsInjectWorker, true)
}

// listConstraints returns all operational constraints for this task (allow before deny).
func (s *Server) listConstraints(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "task not found")
		return
	}
	rows, err := t.Store.ListConstraints()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"constraints": constraintDTOs(rows)})
}

// addConstraint lets a user add an operational constraint (kind=allow|deny). It does not notify the planner.
func (s *Server) addConstraint(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "task not found")
		return
	}
	if !s.engine.beginTaskOperation(t.ID) {
		writeErr(w, 409, errConstraintTaskDeletingAdd)
		return
	}
	defer s.engine.decInflight(t.ID)

	var body struct {
		Text string `json:"text"`
		Kind string `json:"kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid JSON")
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" {
		writeErr(w, 400, errConstraintTextEmpty)
		return
	}
	kind := normalizeConstraintKind(body.Kind)
	if kind == "" {
		writeErr(w, 400, errConstraintKindInvalid)
		return
	}
	id, err := t.Store.AddConstraint(kind, text, "human")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, ConstraintDTO{ID: strconv.FormatInt(id, 10), Kind: kind, Text: text, Origin: "human"})
}

// editConstraint lets a user edit a constraint (kind + text). It does not notify the planner.
func (s *Server) editConstraint(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "task not found")
		return
	}
	if !s.engine.beginTaskOperation(t.ID) {
		writeErr(w, 409, errConstraintTaskDeletingEdit)
		return
	}
	defer s.engine.decInflight(t.ID)

	cid, err := strconv.ParseInt(r.PathValue("cid"), 10, 64)
	if err != nil || cid <= 0 {
		writeErr(w, 400, "bad constraint id")
		return
	}
	var body struct {
		Text string `json:"text"`
		Kind string `json:"kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid JSON")
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" {
		writeErr(w, 400, errConstraintTextEmpty)
		return
	}
	kind := normalizeConstraintKind(body.Kind)
	if kind == "" {
		writeErr(w, 400, errConstraintKindInvalid)
		return
	}
	if err := t.Store.UpdateConstraint(cid, kind, text); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, ConstraintDTO{ID: strconv.FormatInt(cid, 10), Kind: kind, Text: text})
}

// deleteConstraint lets a user delete a constraint. It does not notify the planner.
func (s *Server) deleteConstraint(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "task not found")
		return
	}
	if !s.engine.beginTaskOperation(t.ID) {
		writeErr(w, 409, errConstraintTaskDeletingDelete)
		return
	}
	defer s.engine.decInflight(t.ID)

	cid, err := strconv.ParseInt(r.PathValue("cid"), 10, 64)
	if err != nil || cid <= 0 {
		writeErr(w, 400, "bad constraint id")
		return
	}
	if err := t.Store.DeleteConstraint(cid); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// normalizeConstraintKind lowercases + validates the kind; "" on invalid.
func normalizeConstraintKind(k string) string {
	k = strings.TrimSpace(strings.ToLower(k))
	if k == "allow" || k == "deny" {
		return k
	}
	return ""
}

// constraintDTOs converts db rows to the frontend shape.
func constraintDTOs(in []db.Constraint) []ConstraintDTO {
	out := make([]ConstraintDTO, 0, len(in))
	for _, c := range in {
		out = append(out, ConstraintDTO{
			ID:     strconv.FormatInt(c.ID, 10),
			Kind:   c.Kind,
			Text:   c.Text,
			Origin: c.Origin,
			TS:     rfc3339(c.CreatedAt),
		})
	}
	return out
}
