package email

import (
	"bytes"
	"embed"
	"fmt"
	htmltemplate "html/template"
	"net/url"
	"strings"
	texttemplate "text/template"
)

//go:embed templates/*.html templates/*.txt
var templateFS embed.FS

var (
	htmlTemplates = htmltemplate.Must(htmltemplate.ParseFS(templateFS, "templates/*.html"))
	textTemplates = texttemplate.Must(texttemplate.ParseFS(templateFS, "templates/*.txt"))
)

const (
	verifyPath = "/verify"
	resetPath  = "/reset-password"
	invitePath = "/invite"
)

type verificationData struct {
	Link string
}

type resetData struct {
	Email string
	Link  string
}

type inviteData struct {
	InviterName string
	RankingName string
	RoleName    string
	Link        string
}

func Link(baseURL, path, plaintextToken string) string {
	return strings.TrimRight(baseURL, "/") + path + "?token=" + url.QueryEscape(plaintextToken)
}

func VerificationMessage(to, baseURL, plaintextToken string) (Message, error) {
	data := verificationData{Link: Link(baseURL, verifyPath, plaintextToken)}
	return render("verification", "Verify your email for Rank Anything", to, data)
}

func PasswordResetMessage(to, baseURL, plaintextToken string) (Message, error) {
	data := resetData{Email: to, Link: Link(baseURL, resetPath, plaintextToken)}
	return render("reset", "Reset your Rank Anything password", to, data)
}

func InviteMessage(to, inviterName, rankingName, role, baseURL, plaintextToken string) (Message, error) {
	roleName := "view"
	if role == "EDITOR" {
		roleName = "edit"
	}
	data := inviteData{
		InviterName: inviterName,
		RankingName: rankingName,
		RoleName:    roleName,
		Link:        Link(baseURL, invitePath, plaintextToken),
	}
	return render("invite", inviterName+" shared a ranking with you", to, data)
}

func render(name, subject, to string, data any) (Message, error) {
	var htmlBuf bytes.Buffer
	if err := htmlTemplates.ExecuteTemplate(&htmlBuf, name+".html", data); err != nil {
		return Message{}, fmt.Errorf("email: render %s html: %w", name, err)
	}

	var textBuf bytes.Buffer
	if err := textTemplates.ExecuteTemplate(&textBuf, name+".txt", data); err != nil {
		return Message{}, fmt.Errorf("email: render %s text: %w", name, err)
	}

	return Message{To: to, Subject: subject, HTML: htmlBuf.String(), Text: textBuf.String()}, nil
}
