package service

import (
	"fmt"
	"log/slog"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/account-service/internal/config"
	"gopkg.in/gomail.v2"
)

type NotificationService struct {
	cfg *config.Config
}

func NewNotificationService(cfg *config.Config) *NotificationService {
	return &NotificationService{cfg: cfg}
}

func (s *NotificationService) SendAccountCreatedEmail(toEmail, clientName, brojRacuna, tip, valuta string) error {
	tipLabel := "Tekući"
	if tip == "devizni" {
		tipLabel = "Devizni"
	}

	body := fmt.Sprintf(`
<!DOCTYPE html><html><body>
<h2>Novi račun otvoren!</h2>
<p>Poštovani %s,</p>
<p>Obaveštavamo Vas da je uspešno otvoren novi račun:</p>
<table style="border-collapse:collapse;margin:16px 0;">
  <tr><td style="padding:6px 16px 6px 0;font-weight:bold;">Broj računa:</td><td>%s</td></tr>
  <tr><td style="padding:6px 16px 6px 0;font-weight:bold;">Tip:</td><td>%s</td></tr>
  <tr><td style="padding:6px 16px 6px 0;font-weight:bold;">Valuta:</td><td>%s</td></tr>
</table>
<p>Srdačan pozdrav,<br/>EXBanka</p>
</body></html>
`, clientName, brojRacuna, tipLabel, valuta)

	return s.sendEmail(toEmail, "Novi račun uspešno otvoren — EXBanka", body)
}

func (s *NotificationService) SendCardCreatedEmail(toEmail, clientName, brojKartice, vrstaKartice string) error {
	masked := brojKartice
	if len(brojKartice) == 16 {
		masked = brojKartice[:4] + "********" + brojKartice[12:]
	}
	body := fmt.Sprintf(`
<!DOCTYPE html><html><body>
<h2>Nova kartica izdata!</h2>
<p>Poštovani %s,</p>
<p>Obaveštavamo Vas da je uspešno izdata nova platna kartica:</p>
<table style="border-collapse:collapse;margin:16px 0;">
  <tr><td style="padding:6px 16px 6px 0;font-weight:bold;">Broj kartice:</td><td>%s</td></tr>
  <tr><td style="padding:6px 16px 6px 0;font-weight:bold;">Vrsta:</td><td>%s</td></tr>
</table>
<p>Srdačan pozdrav,<br/>EXBanka</p>
</body></html>
`, clientName, masked, vrstaKartice)

	return s.sendEmail(toEmail, "Nova kartica izdata — EXBanka", body)
}

func (s *NotificationService) sendEmail(to, subject, htmlBody string) error {
	m := gomail.NewMessage()
	m.SetHeader("From", s.cfg.SMTPFrom)
	m.SetHeader("To", to)
	m.SetHeader("Subject", subject)
	m.SetBody("text/html", htmlBody)

	d := gomail.NewDialer(s.cfg.SMTPHost, s.cfg.SMTPPort, "", "")

	if err := d.DialAndSend(m); err != nil {
		slog.Error("SMTP failed", "to", to, "error", err)
		return fmt.Errorf("failed to send email: %w", err)
	}

	slog.Info("Email sent", "subject", subject, "to", to)
	return nil
}
