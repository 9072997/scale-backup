package main

import (
	"bytes"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"strconv"
)

func Email(subject, body string) error {
	debugReturn := DebugCall(subject, body)

	if !SMTPConfigured() {
		debugReturn(nil)
		return nil
	}

	var msg bytes.Buffer
	msg.WriteString("To: " + Config.SMTP.To + "\r\n")
	msg.WriteString("From: " + Config.SMTP.From + "\r\n")
	msg.WriteString("Subject: " + subject + "\r\n")
	msg.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(body + "\r\n")

	err := smtp.SendMail(
		net.JoinHostPort(Config.SMTP.Host, strconv.Itoa(Config.SMTP.Port)),
		nil,
		Config.SMTP.From,
		[]string{Config.SMTP.To},
		msg.Bytes(),
	)
	if err != nil {
		// email is almost always an error, so handle email errors
		// here to avoid huge error handling code everywhere else.
		fmt.Fprintf(os.Stderr, "Email failed: %s\n", err)

		debugReturn(err)
		return err
	}

	debugReturn(nil)
	return nil
}
