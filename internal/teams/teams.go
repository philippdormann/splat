// Package teams owns team/group management and project access grants.
//
// Access model: instance admins (auth.RoleAdmin) bypass every check.
// Members see a project only when granted directly (project_members) or
// through a team (project_teams + team_members). There are no per-project
// roles: membership is binary (access or not); instance administration
// stays with admins.
package teams

import (
	"errors"
	"strings"

	"github.com/philippdormann/splat/internal/database"
)

type Store struct{ db *database.DB }

func NewStore(db *database.DB) *Store { return &Store{db: db} }

type Team struct {
	ID        int64
	Name      string
	Slug      string
	CreatedAt string
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == ' ':
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	return out
}

func (s *Store) Create(name, slug string) (Team, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Team{}, errors.New("team name is required")
	}
	if slug == "" {
		slug = slugify(name)
	} else {
		slug = slugify(slug)
	}
	if slug == "" {
		return Team{}, errors.New("team slug is required")
	}
	if s.db.IsPostgres() {
		var t Team
		err := s.db.QueryRow(
			`INSERT INTO teams (name, slug) VALUES (?, ?) RETURNING id, name, slug, created_at`,
			name, slug).Scan(&t.ID, &t.Name, &t.Slug, &t.CreatedAt)
		if err != nil {
			return Team{}, teamCreateErr(err)
		}
		return t, nil
	}
	res, err := s.db.Exec(`INSERT INTO teams (name, slug) VALUES (?, ?)`, name, slug)
	if err != nil {
		return Team{}, teamCreateErr(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Team{}, err
	}
	return Team{ID: id, Name: name, Slug: slug}, nil
}

func teamCreateErr(err error) error {
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique") {
		return errors.New("team slug already taken")
	}
	return err
}

func (s *Store) List() ([]Team, error) {
	rows, err := s.db.Query(`SELECT id, name, slug, created_at FROM teams ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Team{}
	for rows.Next() {
		var t Team
		if err := rows.Scan(&t.ID, &t.Name, &t.Slug, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) Delete(id int64) error {
	_, err := s.db.Exec(`DELETE FROM teams WHERE id = ?`, id)
	return err
}

func (s *Store) AddMember(teamID, userID int64) error {
	if _, err := s.db.Exec(`INSERT INTO team_members (team_id, user_id) VALUES (?, ?)`, teamID, userID); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil // idempotent
		}
		return err
	}
	return nil
}

func (s *Store) RemoveMember(teamID, userID int64) error {
	_, err := s.db.Exec(`DELETE FROM team_members WHERE team_id = ? AND user_id = ?`, teamID, userID)
	return err
}

func (s *Store) TeamMembers(teamID int64) ([]int64, error) {
	return s.idList(`SELECT user_id FROM team_members WHERE team_id = ? ORDER BY user_id`, teamID)
}

func (s *Store) UserTeams(userID int64) ([]Team, error) {
	rows, err := s.db.Query(`SELECT t.id, t.name, t.slug, t.created_at FROM teams t
		JOIN team_members m ON m.team_id = t.id WHERE m.user_id = ? ORDER BY t.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Team{}
	for rows.Next() {
		var t Team
		if err := rows.Scan(&t.ID, &t.Name, &t.Slug, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) AddProjectMember(projectID, userID int64) error {
	if _, err := s.db.Exec(`INSERT INTO project_members (project_id, user_id) VALUES (?, ?)`, projectID, userID); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil
		}
		return err
	}
	return nil
}

func (s *Store) RemoveProjectMember(projectID, userID int64) error {
	_, err := s.db.Exec(`DELETE FROM project_members WHERE project_id = ? AND user_id = ?`, projectID, userID)
	return err
}

func (s *Store) ProjectMembers(projectID int64) ([]int64, error) {
	return s.idList(`SELECT user_id FROM project_members WHERE project_id = ? ORDER BY user_id`, projectID)
}

func (s *Store) GrantProject(teamID, projectID int64) error {
	if _, err := s.db.Exec(`INSERT INTO project_teams (project_id, team_id) VALUES (?, ?)`, projectID, teamID); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil
		}
		return err
	}
	return nil
}

func (s *Store) RevokeProject(teamID, projectID int64) error {
	_, err := s.db.Exec(`DELETE FROM project_teams WHERE project_id = ? AND team_id = ?`, projectID, teamID)
	return err
}

func (s *Store) ProjectTeams(projectID int64) ([]Team, error) {
	rows, err := s.db.Query(`SELECT t.id, t.name, t.slug, t.created_at FROM teams t
		JOIN project_teams g ON g.team_id = t.id WHERE g.project_id = ? ORDER BY t.name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Team{}
	for rows.Next() {
		var t Team
		if err := rows.Scan(&t.ID, &t.Name, &t.Slug, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) TeamProjects(teamID int64) ([]int64, error) {
	return s.idList(`SELECT project_id FROM project_teams WHERE team_id = ? ORDER BY project_id`, teamID)
}

// CanAccess reports whether userID may touch projectID via a direct grant
// or any shared team.
func (s *Store) CanAccess(userID, projectID int64) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM project_members WHERE project_id = ? AND user_id = ?`,
		projectID, userID).Scan(&n)
	if err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	err = s.db.QueryRow(`SELECT COUNT(*) FROM project_teams g
		JOIN team_members m ON m.team_id = g.team_id
		WHERE g.project_id = ? AND m.user_id = ?`, projectID, userID).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// AccessibleProjects returns every project id the user may touch (direct
// grants plus team grants), sorted.
func (s *Store) AccessibleProjects(userID int64) ([]int64, error) {
	rows, err := s.db.Query(`SELECT project_id FROM project_members WHERE user_id = ?
		UNION SELECT g.project_id FROM project_teams g
		JOIN team_members m ON m.team_id = g.team_id WHERE m.user_id = ?
		ORDER BY 1`, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) idList(query string, arg int64) ([]int64, error) {
	rows, err := s.db.Query(query, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
