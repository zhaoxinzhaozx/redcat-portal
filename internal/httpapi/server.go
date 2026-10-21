package httpapi

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"html"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"portal/internal/mask"
	"portal/internal/phase"
	"portal/internal/store"
	"portal/internal/sub2api"
)

//go:embed web
var webFS embed.FS

type Download struct {
	Step   int    `json:"step"`
	Button string `json:"button"`
	Label  string `json:"label"`
	URL    string `json:"url"`
}

type Site struct {
	Name      string
	Slogan    string
	Contact   string
	URL       string
	Downloads []Download
}

type Server struct {
	Base     string
	Site     Site
	Store    StoreAPI
	Auth     *sub2api.Client
	Location *time.Location
}

type StoreAPI interface {
	ListVisible(ctx context.Context, now time.Time) ([]store.Activity, error)
	ListAdmin(ctx context.Context) ([]store.Activity, error)
	Get(ctx context.Context, id int64, withParticipants bool) (store.Activity, error)
	Create(ctx context.Context, in store.Input) (int64, error)
	Update(ctx context.Context, id int64, in store.Input) error
	Join(ctx context.Context, activityID, userID int64, email string, now time.Time) error
	Joined(ctx context.Context, activityID, userID int64) (bool, error)
	Bump(ctx context.Context, activityID int64) (int, error)
	SetWinners(ctx context.Context, activityID int64, userIDs []int64) error
	Archive(ctx context.Context, activityID int64) error
	Claim(ctx context.Context, activityID, userID int64, now time.Time, credit func(amount float64) error) error
	RechargedAmount(ctx context.Context, userID int64) (float64, error)
}

func New(base string, data StoreAPI, auth *sub2api.Client, site Site) *Server {
	if base == "" {
		base = "/portal"
	}
	if !strings.HasPrefix(base, "/") {
		base = "/" + base
	}
	base = strings.TrimRight(base, "/")
	if strings.TrimSpace(site.Name) == "" {
		site.Name = "站点"
	}
	site.URL = strings.TrimRight(strings.TrimSpace(site.URL), "/")
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	return &Server{Base: base, Site: site, Store: data, Auth: auth, Location: loc}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(webFS, "web")
	fileServer := http.FileServer(http.FS(static))
	mux.Handle(s.Base+"/web/", http.StripPrefix(s.Base+"/web/", fileServer))
	mux.HandleFunc(s.Base+"/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc(s.Base+"/api/", s.api)
	mux.HandleFunc(s.Base+"/", s.page)
	mux.HandleFunc(s.Base, s.page)
	return mux
}

func (s *Server) page(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	raw, err := webFS.ReadFile("web/index.html")
	if err != nil {
		http.Error(w, "page", http.StatusInternalServerError)
		return
	}
	page := strings.ReplaceAll(string(raw), "__BASE__", s.Base)
	page = strings.ReplaceAll(page, "__SITE_NAME__", html.EscapeString(s.Site.Name))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(page))
}

func (s *Server) api(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, s.Base+"/api")
	path = strings.Trim(path, "/")
	parts := []string{}
	if path != "" {
		parts = strings.Split(path, "/")
	}
	switch {
	case r.Method == http.MethodGet && path == "site":
		downloads := s.Site.Downloads
		if downloads == nil {
			downloads = []Download{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"name":      s.Site.Name,
			"slogan":    s.Site.Slogan,
			"contact":   s.Site.Contact,
			"url":       s.Site.URL,
			"downloads": downloads,
		})
	case r.Method == http.MethodGet && path == "me":
		s.me(w, r)
	case r.Method == http.MethodGet && path == "activities":
		s.userActivities(w, r)
	case r.Method == http.MethodGet && len(parts) == 2 && parts[0] == "activities":
		s.userActivity(w, r, parts[1])
	case r.Method == http.MethodPost && len(parts) == 3 && parts[0] == "activities" && parts[2] == "join":
		s.join(w, r, parts[1])
	case r.Method == http.MethodPost && len(parts) == 3 && parts[0] == "activities" && parts[2] == "claim":
		s.claim(w, r, parts[1])
	case r.Method == http.MethodGet && path == "admin/activities":
		s.adminActivities(w, r)
	case r.Method == http.MethodPost && path == "admin/activities":
		s.adminSave(w, r, 0)
	case r.Method == http.MethodGet && len(parts) == 3 && parts[0] == "admin" && parts[1] == "activities":
		s.adminActivity(w, r, parts[2])
	case r.Method == http.MethodPost && len(parts) == 3 && parts[0] == "admin" && parts[1] == "activities":
		id := mustID(parts[2])
		if id == 0 {
			writeError(w, http.StatusNotFound, "活动不存在")
			return
		}
		s.adminSave(w, r, id)
	case r.Method == http.MethodPost && len(parts) == 4 && parts[0] == "admin" && parts[3] == "bump":
		s.adminBump(w, r, parts[2])
	case r.Method == http.MethodPost && len(parts) == 4 && parts[0] == "admin" && parts[3] == "winner":
		s.adminWinner(w, r, parts[2])
	case r.Method == http.MethodPost && len(parts) == 4 && parts[0] == "admin" && parts[3] == "archive":
		s.adminArchive(w, r, parts[2])
	default:
		writeError(w, http.StatusNotFound, "没有这个接口")
	}
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		if sub2api.IsUnauthorized(err) {
			writeJSON(w, http.StatusOK, map[string]any{"logged_in": false})
			return
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"logged_in": true,
		"id":        user.ID,
		"email":     user.Email,
		"admin":     user.IsAdmin(),
	})
}

func (s *Server) userActivities(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListVisible(r.Context(), time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "活动列表读取失败")
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, s.publicActivity(item, 0, false, 0))
	}
	writeJSON(w, http.StatusOK, map[string]any{"activities": out})
}

func (s *Server) userActivity(w http.ResponseWriter, r *http.Request, rawID string) {
	activity, err := s.loadVisible(r, rawID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var userID int64
	joined := false
	recharged := 0.0
	if user, authErr := s.currentUser(r); authErr == nil {
		userID = user.ID
		joined, _ = s.Store.Joined(r.Context(), activity.ID, user.ID)
		recharged, _ = s.Store.RechargedAmount(r.Context(), user.ID)
	}
	writeJSON(w, http.StatusOK, s.publicActivity(activity, userID, joined, recharged))
}

func (s *Server) loadVisible(r *http.Request, rawID string) (store.Activity, error) {
	id := mustID(rawID)
	activity, err := s.Store.Get(r.Context(), id, false)
	if err != nil {
		return store.Activity{}, err
	}
	if !phase.VisibleToUser(time.Now(), activity.StartAt, activity.ArchivedAt != nil) {
		return store.Activity{}, errHidden
	}
	return activity, nil
}

var errHidden = errors.New("活动不存在")

func (s *Server) join(w http.ResponseWriter, r *http.Request, rawID string) {
	user, err := s.currentUser(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	id := mustID(rawID)
	if err := s.Store.Join(r.Context(), id, user.ID, user.Email, time.Now()); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	activity, err := s.Store.Get(r.Context(), id, false)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"participant_count": activity.DisplayCount(), "joined": true})
}

func (s *Server) claim(w http.ResponseWriter, r *http.Request, rawID string) {
	user, err := s.currentUser(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	id := mustID(rawID)
	err = s.Store.Claim(r.Context(), id, user.ID, time.Now(), func(amount float64) error {
		return s.Auth.AddBalance(r.Context(), user.ID, amount, id, s.Site.Name)
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"claimed": true})
}

func (s *Server) adminActivities(w http.ResponseWriter, r *http.Request) {
	if _, err := s.adminUser(r); err != nil {
		writeAuthError(w, err)
		return
	}
	items, err := s.Store.ListAdmin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "活动列表读取失败")
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, s.adminJSON(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"activities": out})
}

func (s *Server) adminActivity(w http.ResponseWriter, r *http.Request, rawID string) {
	if _, err := s.adminUser(r); err != nil {
		writeAuthError(w, err)
		return
	}
	activity, err := s.Store.Get(r.Context(), mustID(rawID), true)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.adminJSON(activity))
}

func (s *Server) adminSave(w http.ResponseWriter, r *http.Request, id int64) {
	if _, err := s.adminUser(r); err != nil {
		writeAuthError(w, err)
		return
	}
	in, err := s.readInput(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if id == 0 {
		newID, err := s.Store.Create(r.Context(), in)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": newID})
		return
	}
	if err := s.Store.Update(r.Context(), id, in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (s *Server) adminBump(w http.ResponseWriter, r *http.Request, rawID string) {
	if _, err := s.adminUser(r); err != nil {
		writeAuthError(w, err)
		return
	}
	count, err := s.Store.Bump(r.Context(), mustID(rawID))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"participant_count": count})
}

func (s *Server) adminWinner(w http.ResponseWriter, r *http.Request, rawID string) {
	if _, err := s.adminUser(r); err != nil {
		writeAuthError(w, err)
		return
	}
	var body struct {
		UserIDs []int64 `json:"user_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请选择中奖人")
		return
	}
	id := mustID(rawID)
	if err := s.Store.SetWinners(r.Context(), id, body.UserIDs); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) adminArchive(w http.ResponseWriter, r *http.Request, rawID string) {
	if _, err := s.adminUser(r); err != nil {
		writeAuthError(w, err)
		return
	}
	if err := s.Store.Archive(r.Context(), mustID(rawID)); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) publicActivity(activity store.Activity, userID int64, joined bool, recharged float64) map[string]any {
	now := time.Now()
	current := activity.Phase(now)
	showWinner := current == phase.Announced
	winners := []string{}
	if showWinner {
		for _, winner := range activity.Winners {
			winners = append(winners, mask.Email(winner.Email))
		}
	}
	canClaim := showWinner && activity.Winner(userID) && !activity.ClaimedBy(userID)
	payload := map[string]any{
		"id":                 activity.ID,
		"title":              activity.Title,
		"body":               activity.Body,
		"reward_amount":      activity.RewardAmount,
		"recharge_threshold": activity.RechargeThreshold,
		"start_at":           activity.StartAt,
		"end_at":             activity.EndAt,
		"announce_at":        activity.AnnounceAt,
		"phase":              current,
		"participant_count":  activity.DisplayCount(),
		"joined":             joined,
		"winners":            winners,
		"can_claim":          canClaim,
		"claimed":            activity.ClaimedBy(userID),
		"has_winner":         showWinner && len(activity.Winners) > 0,
	}
	if userID != 0 {
		payload["recharged_amount"] = recharged
		payload["eligible"] = recharged+0.000001 >= activity.RechargeThreshold
	}
	return payload
}

func (s *Server) adminJSON(activity store.Activity) map[string]any {
	participants := make([]map[string]any, 0, len(activity.Participants))
	for _, participant := range activity.Participants {
		participants = append(participants, map[string]any{
			"user_id": participant.UserID,
			"email":   participant.Email,
		})
	}
	if activity.SelectedIDs == nil {
		activity.SelectedIDs = []int64{}
	}
	winners := make([]map[string]any, 0, len(activity.Winners))
	for _, winner := range activity.Winners {
		winners = append(winners, map[string]any{
			"user_id": winner.UserID,
			"email":   winner.Email,
			"claimed": activity.ClaimedBy(winner.UserID),
		})
	}
	return map[string]any{
		"id":                 activity.ID,
		"title":              activity.Title,
		"body":               activity.Body,
		"reward_amount":      activity.RewardAmount,
		"recharge_threshold": activity.RechargeThreshold,
		"winner_count":       activity.WinnerCount,
		"start_at":           activity.StartAt,
		"end_at":             activity.EndAt,
		"announce_at":        activity.AnnounceAt,
		"phase":              activity.Phase(time.Now()),
		"participant_count":  activity.DisplayCount(),
		"real_count":         activity.RealCount,
		"display_bonus":      activity.DisplayBonus,
		"selected_user_ids":  activity.SelectedIDs,
		"winners":            winners,
		"drawn":              activity.DrawnAt != nil,
		"archived":           activity.ArchivedAt != nil,
		"participants":       participants,
	}
}

func (s *Server) readInput(r *http.Request) (store.Input, error) {
	var body struct {
		Title             string  `json:"title"`
		Body              string  `json:"body"`
		RewardAmount      float64 `json:"reward_amount"`
		RechargeThreshold float64 `json:"recharge_threshold"`
		StartAt           string  `json:"start_at"`
		EndAt             string  `json:"end_at"`
		AnnounceAt        string  `json:"announce_at"`
		WinnerCount       int     `json:"winner_count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return store.Input{}, errors.New("提交内容无法识别")
	}
	start, err1 := parseTime(body.StartAt, s.Location)
	end, err2 := parseTime(body.EndAt, s.Location)
	announce, err3 := parseTime(body.AnnounceAt, s.Location)
	if err1 != nil || err2 != nil || err3 != nil {
		return store.Input{}, errors.New("请填写完整的开始、结束和公布时间")
	}
	return store.Input{
		Title:             strings.TrimSpace(body.Title),
		Body:              strings.TrimSpace(body.Body),
		RewardAmount:      body.RewardAmount,
		RechargeThreshold: body.RechargeThreshold,
		StartAt:           start,
		EndAt:             end,
		AnnounceAt:        announce,
		WinnerCount:       body.WinnerCount,
	}, nil
}

func parseTime(value string, loc *time.Location) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, errors.New("empty")
	}
	if len(value) == len("2006-01-02T15:04") {
		return time.ParseInLocation("2006-01-02T15:04", value, loc)
	}
	return time.Parse(time.RFC3339, value)
}

func (s *Server) currentUser(r *http.Request) (sub2api.User, error) {
	token := bearer(r)
	if token == "" {
		return sub2api.User{}, sub2api.ErrUnauthorized()
	}
	return s.Auth.Me(r.Context(), token)
}

func (s *Server) adminUser(r *http.Request) (sub2api.User, error) {
	user, err := s.currentUser(r)
	if err != nil {
		return sub2api.User{}, err
	}
	if !user.IsAdmin() {
		return sub2api.User{}, errForbidden
	}
	return user, nil
}

var errForbidden = errors.New("需要管理员登录")

func bearer(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if len(header) > 7 && strings.EqualFold(header[:7], "bearer ") {
		return strings.TrimSpace(header[7:])
	}
	return ""
}

func mustID(raw string) int64 {
	id, _ := strconv.ParseInt(raw, 10, 64)
	return id
}

func writeAuthError(w http.ResponseWriter, err error) {
	if sub2api.IsUnauthorized(err) {
		writeError(w, http.StatusUnauthorized, "请先登录")
		return
	}
	if errors.Is(err, errForbidden) {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	writeError(w, http.StatusBadGateway, err.Error())
}

func writeStoreError(w http.ResponseWriter, err error) {
	if store.IsNotFound(err) || errors.Is(err, errHidden) {
		writeError(w, http.StatusNotFound, "活动不存在")
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
