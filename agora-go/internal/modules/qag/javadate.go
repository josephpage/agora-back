package qag

import (
	"time"

	"agora/internal/common"
)

// localDate is common.LocalDate (Jackson's LocalDateDeserializer).
type localDate = common.LocalDate

// formatDate is DateMapper.toFormattedDate(Date).
func formatDate(t time.Time) string { return common.FormatDate(t) }
