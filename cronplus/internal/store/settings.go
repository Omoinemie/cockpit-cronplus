package store

import "cronplus/pkg/model"

// ReadSettings reads settings from settings.json, merged with defaults.
func (s *Store) ReadSettings() (model.Settings, error) {
	settings := model.DefaultSettings()
	_, err := ReadJSON(s.SettingsPath, &settings)
	return settings, err
}

// WriteSettings writes settings to settings.json.
func (s *Store) WriteSettings(settings model.Settings) error {
	return writeJSON(s.SettingsPath, settings)
}
