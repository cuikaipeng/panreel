package storage

import (
	"database/sql"
	"encoding/json"
	"github.com/cuikaipeng/panreel/internal/model"
)

func (s *Store) Preferences(legacyTheme string) (model.Preferences, error) {
	p := model.DefaultPreferences()
	p.Theme = legacyTheme
	var data string
	err := s.db.QueryRow("SELECT data FROM desktop_preferences WHERE id=1").Scan(&data)
	if err == sql.ErrNoRows {
		// Versions before 0.3 displayed Chinese unconditionally.
		var count int
		if e := s.db.QueryRow("SELECT COUNT(*) FROM preferences").Scan(&count); e != nil {
			return p, e
		} else if count > 0 {
			p.Language = "zh-Hans"
		}
		p.Normalize()
		return p, nil
	}
	if err != nil {
		return p, err
	}
	if err = json.Unmarshal([]byte(data), &p); err != nil {
		return p, err
	}
	p.Normalize()
	return p, nil
}

func (s *Store) SavePreferences(p model.Preferences) error {
	p.Normalize()
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO desktop_preferences(id,data) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data", string(data))
	return err
}
