package response

import (
	"github.com/minhhoccode111/fileuploader/config"
)

// SettingsRow is one line of the read-only defaults table of the admin
// settings page. Value is already worded for the screen.
type SettingsRow struct {
	Name  string
	Value string
	Note  string
}

// SettingsRows words the environment-configured limits the admin settings
// page shows. They are the defaults the per-account values of the account
// editor inherit from, so an admin knows what a saved zero means before
// saving an account.
func SettingsRows(cfg *config.Config) []SettingsRow {
	return []SettingsRow{
		{
			Name:  "Default storage quota",
			Value: limitLabel(cfg.Limits.StorageQuotaBytes),
			Note:  "A per-account zero inherits this default.",
		},
		{
			Name:  "Default upload rate",
			Value: rateLabel(cfg.Limits.UploadRateBytesPerSec),
			Note:  "Paces each upload part of the account.",
		},
		{
			Name:  "Default download rate",
			Value: rateLabel(cfg.Limits.DownloadRateBytesPerSec),
			Note:  "Shared by every download the account runs at once.",
		},
		{
			Name:  "Max upload file size",
			Value: limitLabel(cfg.Upload.MaxBytes),
			Note:  "Set by UPLOAD_MAX_BYTES in the environment; the per-account limit is the quota.",
		},
	}
}

// limitLabel words a byte limit for the screen. A zero means no limit, which
// is what the drive inherits when neither the account nor the environment
// names one.
func limitLabel(limit int64) string {
	if limit == 0 {
		return "Unlimited"
	}

	return ByteLabel(limit)
}

// rateLabel words a per-second byte rate for the screen.
func rateLabel(rate int64) string {
	if rate == 0 {
		return "Unlimited"
	}

	return ByteLabel(rate) + "/s"
}
