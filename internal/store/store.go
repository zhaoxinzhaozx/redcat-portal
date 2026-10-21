package store

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"

	"portal/internal/draw"
	"portal/internal/phase"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const schemaSQL = `
CREATE SCHEMA IF NOT EXISTS {{schema}};

CREATE TABLE IF NOT EXISTS activities (
  id BIGSERIAL PRIMARY KEY,
  title TEXT NOT NULL,
  body TEXT NOT NULL DEFAULT '',
  reward_amount NUMERIC(20,2) NOT NULL,
  recharge_threshold NUMERIC(20,2) NOT NULL,
  start_at TIMESTAMPTZ NOT NULL,
  end_at TIMESTAMPTZ NOT NULL,
  announce_at TIMESTAMPTZ NOT NULL,
  display_bonus INTEGER NOT NULL DEFAULT 0,
  winner_count INTEGER NOT NULL DEFAULT 1,
  selected_user_id BIGINT,
  winner_user_id BIGINT,
  winner_email TEXT,
  drawn_at TIMESTAMPTZ,
  archived_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS participants (
  activity_id BIGINT NOT NULL REFERENCES activities(id),
  user_id BIGINT NOT NULL,
  email TEXT NOT NULL,
  joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (activity_id, user_id)
);

CREATE TABLE IF NOT EXISTS claims (
  activity_id BIGINT NOT NULL REFERENCES activities(id),
  user_id BIGINT NOT NULL,
  amount NUMERIC(20,2) NOT NULL,
  claimed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (activity_id, user_id)
);

CREATE TABLE IF NOT EXISTS selections (
  activity_id BIGINT NOT NULL REFERENCES activities(id),
  user_id BIGINT NOT NULL,
  PRIMARY KEY (activity_id, user_id)
);

CREATE TABLE IF NOT EXISTS winners (
  activity_id BIGINT NOT NULL REFERENCES activities(id),
  user_id BIGINT NOT NULL,
  email TEXT NOT NULL,
  PRIMARY KEY (activity_id, user_id)
);
`

const migrateSQL = `
ALTER TABLE activities ADD COLUMN IF NOT EXISTS winner_count INTEGER NOT NULL DEFAULT 1;
ALTER TABLE claims DROP CONSTRAINT IF EXISTS claims_pkey;
ALTER TABLE claims ADD CONSTRAINT claims_pkey PRIMARY KEY (activity_id, user_id);

INSERT INTO winners (activity_id, user_id, email)
SELECT id, winner_user_id, COALESCE(winner_email, '')
FROM activities
WHERE winner_user_id IS NOT NULL
ON CONFLICT DO NOTHING;

INSERT INTO selections (activity_id, user_id)
SELECT id, selected_user_id
FROM activities
WHERE selected_user_id IS NOT NULL AND drawn_at IS NULL
ON CONFLICT DO NOTHING;
`

// RechargeSQL is read-only. It never writes payment or user rows.
const RechargeSQL = `
SELECT COALESCE(SUM(GREATEST(pay_amount - refund_amount, 0)), 0)::float8
FROM payment_orders
WHERE user_id = $1
  AND order_type = 'balance'
  AND status IN ('COMPLETED', 'PARTIALLY_REFUNDED')
`

type Activity struct {
	ID                int64
	Title             string
	Body              string
	RewardAmount      float64
	RechargeThreshold float64
	StartAt           time.Time
	EndAt             time.Time
	AnnounceAt        time.Time
	DisplayBonus      int
	RealCount         int
	WinnerCount       int
	SelectedIDs       []int64
	Winners           []draw.Participant
	ClaimedIDs        []int64
	DrawnAt           *time.Time
	ArchivedAt        *time.Time
	Participants      []draw.Participant
}

func (a Activity) Winner(userID int64) bool {
	for _, winner := range a.Winners {
		if winner.UserID == userID {
			return true
		}
	}
	return false
}

func (a Activity) ClaimedBy(userID int64) bool {
	for _, id := range a.ClaimedIDs {
		if id == userID {
			return true
		}
	}
	return false
}

func (a Activity) Phase(now time.Time) string {
	return phase.Of(now, a.StartAt, a.EndAt, a.AnnounceAt, a.ArchivedAt != nil)
}

func (a Activity) DisplayCount() int {
	return a.RealCount + a.DisplayBonus
}

type Input struct {
	Title             string
	Body              string
	RewardAmount      float64
	RechargeThreshold float64
	StartAt           time.Time
	EndAt             time.Time
	AnnounceAt        time.Time
	WinnerCount       int
}

type Store struct {
	pool *pgxpool.Pool
}

var schemaPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func Open(ctx context.Context, databaseURL, schema string) (*Store, error) {
	if schema == "" {
		schema = "portal"
	}
	if !schemaPattern.MatchString(schema) {
		return nil, fmt.Errorf("invalid database schema")
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+schema+", public")
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if _, err := pool.Exec(ctx, strings.ReplaceAll(schemaSQL, "{{schema}}", schema)); err != nil {
		pool.Close()
		return nil, fmt.Errorf("create portal tables: %w", err)
	}
	if _, err := pool.Exec(ctx, strings.ReplaceAll(migrateSQL, "{{schema}}", schema)); err != nil {
		pool.Close()
		return nil, fmt.Errorf("update portal tables: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

func (s *Store) RechargedAmount(ctx context.Context, userID int64) (float64, error) {
	var amount float64
	err := s.pool.QueryRow(ctx, RechargeSQL, userID).Scan(&amount)
	return amount, err
}

func (s *Store) Create(ctx context.Context, in Input) (int64, error) {
	if err := validateInput(in); err != nil {
		return 0, err
	}
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO activities (title, body, reward_amount, recharge_threshold, start_at, end_at, announce_at, winner_count)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id
	`, in.Title, in.Body, in.RewardAmount, in.RechargeThreshold, in.StartAt, in.EndAt, in.AnnounceAt, in.WinnerCount).Scan(&id)
	return id, err
}

func (s *Store) Update(ctx context.Context, id int64, in Input) error {
	if err := validateInput(in); err != nil {
		return err
	}
	var selected int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM selections WHERE activity_id = $1`, id).Scan(&selected); err != nil {
		return err
	}
	if selected > in.WinnerCount {
		return errors.New("已选中奖人多于中奖人数，请先减少选择")
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE activities
		SET title = $2, body = $3, reward_amount = $4, recharge_threshold = $5,
		    start_at = $6, end_at = $7, announce_at = $8, winner_count = $9, updated_at = now()
		WHERE id = $1 AND drawn_at IS NULL AND archived_at IS NULL
	`, id, in.Title, in.Body, in.RewardAmount, in.RechargeThreshold, in.StartAt, in.EndAt, in.AnnounceAt, in.WinnerCount)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("活动已开奖或已归入历史，不能再修改")
	}
	return nil
}

func validateInput(in Input) error {
	if in.Title == "" {
		return errors.New("请填写活动名称")
	}
	if in.RewardAmount <= 0 {
		return errors.New("奖励金额需要大于 0")
	}
	if in.RechargeThreshold < 0 {
		return errors.New("充值门槛不能小于 0")
	}
	if !in.EndAt.After(in.StartAt) {
		return errors.New("结束时间需要晚于开始时间")
	}
	if in.AnnounceAt.Before(in.EndAt) {
		return errors.New("公布时间不能早于结束时间")
	}
	if in.WinnerCount < 1 {
		return errors.New("中奖人数至少为 1")
	}
	return nil
}

func (s *Store) ListAdmin(ctx context.Context) ([]Activity, error) {
	return s.list(ctx, `
		SELECT a.id FROM activities a
		ORDER BY a.created_at DESC, a.id DESC
	`)
}

func (s *Store) ListVisible(ctx context.Context, now time.Time) ([]Activity, error) {
	return s.list(ctx, `
		SELECT a.id FROM activities a
		WHERE a.archived_at IS NULL AND a.start_at <= $1
		ORDER BY a.start_at DESC, a.id DESC
	`, now)
}

func (s *Store) list(ctx context.Context, query string, args ...any) ([]Activity, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]Activity, 0, len(ids))
	for _, id := range ids {
		activity, err := s.Get(ctx, id, false)
		if err != nil {
			return nil, err
		}
		out = append(out, activity)
	}
	return out, nil
}

func (s *Store) Get(ctx context.Context, id int64, withParticipants bool) (Activity, error) {
	var activity Activity
	err := s.pool.QueryRow(ctx, `
		SELECT a.id, a.title, a.body, a.reward_amount::float8, a.recharge_threshold::float8,
		       a.start_at, a.end_at, a.announce_at, a.display_bonus, a.winner_count,
		       a.drawn_at, a.archived_at,
		       (SELECT COUNT(*) FROM participants p WHERE p.activity_id = a.id)
		FROM activities a
		WHERE a.id = $1
	`, id).Scan(
		&activity.ID, &activity.Title, &activity.Body, &activity.RewardAmount, &activity.RechargeThreshold,
		&activity.StartAt, &activity.EndAt, &activity.AnnounceAt, &activity.DisplayBonus, &activity.WinnerCount,
		&activity.DrawnAt, &activity.ArchivedAt,
		&activity.RealCount,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Activity{}, errNotFound
	}
	if err != nil {
		return Activity{}, err
	}
	if activity.SelectedIDs, err = s.ids(ctx, `SELECT user_id FROM selections WHERE activity_id = $1 ORDER BY user_id`, id); err != nil {
		return Activity{}, err
	}
	if activity.ClaimedIDs, err = s.ids(ctx, `SELECT user_id FROM claims WHERE activity_id = $1 ORDER BY user_id`, id); err != nil {
		return Activity{}, err
	}
	winnerRows, err := s.pool.Query(ctx, `SELECT user_id, email FROM winners WHERE activity_id = $1 ORDER BY user_id`, id)
	if err != nil {
		return Activity{}, err
	}
	activity.Winners, err = pgx.CollectRows(winnerRows, pgx.RowToStructByPos[draw.Participant])
	if err != nil {
		return Activity{}, err
	}
	if withParticipants {
		rows, err := s.pool.Query(ctx, `
			SELECT user_id, email FROM participants
			WHERE activity_id = $1
			ORDER BY joined_at, user_id
		`, id)
		if err != nil {
			return Activity{}, err
		}
		defer rows.Close()
		for rows.Next() {
			var participant draw.Participant
			if err := rows.Scan(&participant.UserID, &participant.Email); err != nil {
				return Activity{}, err
			}
			activity.Participants = append(activity.Participants, participant)
		}
		if err := rows.Err(); err != nil {
			return Activity{}, err
		}
	}
	return activity, nil
}

var errNotFound = errors.New("活动不存在")

func IsNotFound(err error) bool {
	return errors.Is(err, errNotFound)
}

func (s *Store) Join(ctx context.Context, activityID, userID int64, email string, now time.Time) error {
	activity, err := s.Get(ctx, activityID, false)
	if err != nil {
		return err
	}
	if activity.Phase(now) != phase.Running {
		return errors.New("当前不在参与时间内")
	}
	amount, err := s.RechargedAmount(ctx, userID)
	if err != nil {
		return err
	}
	if amount+0.000001 < activity.RechargeThreshold {
		return errors.New("您暂时无法参与此活动")
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO participants (activity_id, user_id, email)
		VALUES ($1, $2, $3)
		ON CONFLICT (activity_id, user_id) DO NOTHING
	`, activityID, userID, email)
	return err
}

func (s *Store) Joined(ctx context.Context, activityID, userID int64) (bool, error) {
	var joined bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM participants WHERE activity_id = $1 AND user_id = $2)
	`, activityID, userID).Scan(&joined)
	return joined, err
}

func (s *Store) Bump(ctx context.Context, activityID int64) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		UPDATE activities AS a
		SET display_bonus = a.display_bonus + 1, updated_at = now()
		WHERE a.id = $1 AND a.archived_at IS NULL
		RETURNING a.display_bonus + (SELECT COUNT(*) FROM participants p WHERE p.activity_id = a.id)
	`, activityID).Scan(&count)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, errors.New("活动不存在或已归入历史")
	}
	return count, err
}

func (s *Store) ids(ctx context.Context, query string, activityID int64) ([]int64, error) {
	rows, err := s.pool.Query(ctx, query, activityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) SetWinners(ctx context.Context, activityID int64, userIDs []int64) error {
	activity, err := s.Get(ctx, activityID, false)
	if err != nil {
		return err
	}
	if activity.DrawnAt != nil || activity.ArchivedAt != nil {
		return errors.New("开奖后不能再修改中奖人")
	}
	chosen := make([]int64, 0, len(userIDs))
	seen := map[int64]bool{}
	for _, userID := range userIDs {
		if userID == 0 || seen[userID] {
			continue
		}
		seen[userID] = true
		chosen = append(chosen, userID)
	}
	if len(chosen) > activity.WinnerCount {
		return errors.New("选择人数不能超过中奖人数")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, userID := range chosen {
		var joined bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM participants WHERE activity_id = $1 AND user_id = $2)
		`, activityID, userID).Scan(&joined); err != nil {
			return err
		}
		if !joined {
			return errors.New("只能从已参与的用户里选择")
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM selections WHERE activity_id = $1`, activityID); err != nil {
		return err
	}
	for _, userID := range chosen {
		if _, err := tx.Exec(ctx, `INSERT INTO selections (activity_id, user_id) VALUES ($1, $2)`, activityID, userID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE activities SET updated_at = now() WHERE id = $1`, activityID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) Archive(ctx context.Context, activityID int64) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE activities
		SET archived_at = COALESCE(archived_at, now()), updated_at = now()
		WHERE id = $1
	`, activityID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errNotFound
	}
	return nil
}

func (s *Store) DrawDue(ctx context.Context, now time.Time) error {
	rows, err := s.pool.Query(ctx, `
		SELECT id FROM activities
		WHERE drawn_at IS NULL AND archived_at IS NULL AND announce_at <= $1
		ORDER BY id
	`, now)
	if err != nil {
		return err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.drawOne(ctx, id, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) drawOne(ctx context.Context, id int64, now time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var winnerCount int
	var drawn *time.Time
	err = tx.QueryRow(ctx, `
		SELECT winner_count, drawn_at
		FROM activities
		WHERE id = $1
		FOR UPDATE
	`, id).Scan(&winnerCount, &drawn)
	if err != nil {
		return err
	}
	if drawn != nil {
		return tx.Commit(ctx)
	}
	selected, err := queryIDs(ctx, tx, `SELECT user_id FROM selections WHERE activity_id = $1 ORDER BY user_id`, id)
	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `
		SELECT user_id, email FROM participants WHERE activity_id = $1 ORDER BY user_id
	`, id)
	if err != nil {
		return err
	}
	participants, err := pgx.CollectRows(rows, pgx.RowToStructByPos[draw.Participant])
	if err != nil {
		return err
	}
	winners := draw.ChooseWinners(winnerCount, selected, participants, cryptoPick)
	if _, err = tx.Exec(ctx, `
		UPDATE activities
		SET drawn_at = $2, updated_at = now()
		WHERE id = $1
	`, id, now); err != nil {
		return err
	}
	for _, winner := range winners {
		if _, err = tx.Exec(ctx, `
			INSERT INTO winners (activity_id, user_id, email)
			VALUES ($1, $2, $3)
			ON CONFLICT DO NOTHING
		`, id, winner.UserID, winner.Email); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func queryIDs(ctx context.Context, tx pgx.Tx, query string, activityID int64) ([]int64, error) {
	rows, err := tx.Query(ctx, query, activityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func cryptoPick(n int) int {
	if n <= 1 {
		return 0
	}
	value, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0
	}
	return int(value.Int64())
}

func (s *Store) Claim(ctx context.Context, activityID, userID int64, now time.Time, credit func(amount float64) error) error {
	activity, err := s.Get(ctx, activityID, false)
	if err != nil {
		return err
	}
	if activity.Phase(now) != phase.Announced {
		return errors.New("还未到领取时间")
	}
	if !activity.Winner(userID) {
		return errors.New("只有中奖用户可以领取")
	}
	if activity.ClaimedBy(userID) {
		return errors.New("已经领取过了")
	}
	if err := credit(activity.RewardAmount); err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO claims (activity_id, user_id, amount)
		VALUES ($1, $2, $3)
	`, activityID, userID, activity.RewardAmount)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return errors.New("已经领取过了")
		}
		return err
	}
	return nil
}
