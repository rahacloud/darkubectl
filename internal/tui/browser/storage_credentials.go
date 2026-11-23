package browser

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func (m *browser) storageCredentialLines() []string {
	lines := []string{paint("ONE-TIME CREDENTIALS", browserAccent, true),
		summaryField("Key", safeText(m.storageSecret.Name)), summaryField("Endpoint", storageEndpointText(m.storageSecretEndpoint)),
		"", "Closing loses this pair; the key secret cannot be retrieved again.",
		"Terminal scrollback, clipboard managers and screen sharing may retain values."}
	if m.storageSecretVisible {
		lines = append(lines, summaryField("Access", safeText(m.storageSecret.AccessKey)),
			summaryField("Secret", safeText(m.storageSecret.SecretKey)))
	} else {
		lines = append(lines, summaryField("Access", "••••••••••"), summaryField("Secret", "••••••••••"))
	}
	return lines
}

func (m *browser) storageSaveModalLines() []string {
	lines := []string{paint(storageIconKey+"  SAVE CREDENTIALS", browserAccent, true), "", "Destination path"}
	if m.storageDialog == storageDialogSavePath {
		width := max(1, min(storageModalWidth, m.width-dialogOuterMargin)-dialogInnerPadding)
		input := safeText(m.storageDraft.text) + "▏"
		if inputWidth := ansi.StringWidth(input); inputWidth > width {
			remove := inputWidth - width + ansi.StringWidth("…")
			input = ansi.TruncateLeft(input, remove, "…")
		}
		lines = append(lines, paint(input, browserBranch, true))
		if strings.TrimSpace(m.storageDraft.text) == "" {
			return append(lines, "", paint("Use .json, .yaml, .yml or .env", browserMuted, false))
		}
		path, format, err := storageExportPath(m.storageDraft.text, storageHome(), storageWorkingDir())
		if err != nil {
			return lines
		}
		return append(lines, "", summaryField("Format", format), summaryField("Destination", safeText(path)))
	}
	path, format, err := storageExportPath(m.storageDraft.exportPath, storageHome(), storageWorkingDir())
	if err != nil {
		return append(lines, paint(safeText(err.Error()), browserNumber, false))
	}
	return append(lines, safeText(path), "", summaryField("Format", format),
		paint("A new owner-only file will contain your credentials.", browserMuted, false))
}

func storageHome() string       { v, _ := os.UserHomeDir(); return v }
func storageWorkingDir() string { v, _ := os.Getwd(); return v }

func (m *browser) advanceStorageSaveDialog() tea.Cmd {
	if m.storageDialog == storageDialogSavePath {
		if path, format, err := storageExportPath(m.storageDraft.text, storageHome(), storageWorkingDir()); err == nil {
			m.storageNotice = ""
			m.storageDraft.exportPath, m.storageDraft.exportFormat = path, format
			m.storageDialog = storageDialogSaveReview
		} else {
			m.storageNotice = err.Error()
		}
		return nil
	}

	path, format, err := storageExportPath(m.storageDraft.exportPath, storageHome(), storageWorkingDir())
	if err != nil {
		m.storageNotice = err.Error()
		m.storageDialog = storageDialogSavePath
		return nil
	}
	if m.storageSecret == nil {
		m.storageNotice = "Credential result is unavailable"
		m.storageDialog = ""
		return nil
	}
	err = saveStorageCredentials(path, format, storageCredentialExport{
		AccessKey: m.storageSecret.AccessKey, SecretKey: m.storageSecret.SecretKey,
		Endpoint: m.storageSecretEndpoint,
	})
	if err != nil {
		m.storageNotice = err.Error()
		m.storageDialog = storageDialogSavePath
	} else {
		m.storageDialog = ""
		return m.storageFeedbackMessage("Credentials saved to " + path)
	}
	return nil
}
