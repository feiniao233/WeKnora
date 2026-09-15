package repository

import (
	"os"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMessageExecutionResultMigrationAndHistory(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&types.Message{}))
	require.NoError(t, db.Migrator().DropColumn(&types.Message{}, "execution_result"))
	require.NoError(t, db.Exec("INSERT INTO messages (id, content) VALUES ('old', 'existing answer')").Error)
	up, err := os.ReadFile("../../../migrations/sqlite/000014_message_execution_result.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(up)).Error)
	var restored types.Message
	require.NoError(t, db.First(&restored, "id = ?", "old").Error)
	require.Nil(t, restored.ExecutionResult, "older messages must retain unknown outcomes")
	result := &types.MessageExecutionResult{Status: "failed", Error: types.ClassifyExecutionError("unexpected EOF", "request")}
	require.NoError(t, db.Model(&types.Message{}).Where("id = ?", "old").Update("execution_result", result).Error)
	require.NoError(t, db.First(&restored, "id = ?", "old").Error)
	require.Equal(t, result, restored.ExecutionResult)
	require.Equal(t, "existing answer", restored.Content)
	down, err := os.ReadFile("../../../migrations/sqlite/000014_message_execution_result.down.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(down)).Error)
	require.False(t, db.Migrator().HasColumn(&types.Message{}, "execution_result"))
}
