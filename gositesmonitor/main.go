package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type SiteInfo struct {
	URL       string
	Status    int
	Up        bool
	LastCheck time.Time
	FailCount int
}

var allInfos = make(map[string]*SiteInfo)

func lerSitesDoArquivo() []string {
	if len(os.Args) < 2 {
		fmt.Printf("Usage: %s <filename>\n", os.Args[0])
		os.Exit(1)
	}

	filename := os.Args[1]

	arquivo, err := os.Open(filename)
	if err != nil {
		log.Fatal(err)
	}
	defer arquivo.Close()

	var sites []string
	scanner := bufio.NewScanner(arquivo)
	for scanner.Scan() {
		site := strings.TrimSpace(scanner.Text())
		if site != "" {
			sites = append(sites, site)
		}
	}
	return sites
}

func obterSitesForaDoAr() map[string]*SiteInfo {
	downSites := make(map[string]*SiteInfo)
	for url, info := range allInfos {
		if !info.Up {
			downSites[url] = info
		}
	}
	return downSites
}

func verificaSite(url string) *SiteInfo {
	info, exists := allInfos[url]
	if !exists {
		info = &SiteInfo{URL: url}
		allInfos[url] = info
	}

	info.LastCheck = time.Now()
	resp, err := http.Get(url)
	if err != nil {
		info.Up = false
		info.FailCount++
		return info
	}
	defer resp.Body.Close()

	info.Status = resp.StatusCode
	info.Up = resp.StatusCode >= 200 && resp.StatusCode < 300

	if info.Up {
		info.FailCount = 0
	} else {
		info.FailCount++
	}

	return info
}

func exibirEstatisticas() {
	for url, info := range allInfos {
		status := "🔴 OFF"
		if info.Up {
			status = "🟢 ON"
		}
		fmt.Printf("%s - %s (falhas: %d)\n", status, url, info.FailCount)
	}
	fmt.Println("")
}

func totalFalhas(infos map[string]*SiteInfo) int {
	total := 0
	for _, site := range infos {
		total += site.FailCount
	}
	return total
}

func formatarMensagemAlerta(infos map[string]*SiteInfo) string {
	var msg string

	if len(infos) == 1 {
		msg = "🚨 *ALERTA: 1 site indisponível*\n\n"
	} else {
		msg = fmt.Sprintf("🚨 *ALERTA: %d sites indisponíveis*\n\n", len(infos))
	}

	i := 1
	for url, info := range infos {

		icone := "🔴"
		if info.Status >= 500 {
			icone = "🔥" // Erro de servidor é mais crítico
		}

		msg += fmt.Sprintf("%s *%d. %s*\n", icone, i, url)
		msg += fmt.Sprintf("   ├ Status: `%d`\n", info.Status)
		msg += fmt.Sprintf("   ├ Falhas: %d\n", info.FailCount)
		msg += fmt.Sprintf("   └ Última verificação: %s\n\n", info.LastCheck.Format("02/01/2006 15:04:05"))
		i++
	}

	msg += fmt.Sprintf("📊 *Total de falhas:* %d\n", totalFalhas(infos))
	msg += fmt.Sprintf("⏰ *Alerta gerado em:* %s", time.Now().Format("02/01/2006 15:04:05"))

	return msg
}

func enviarAlertaTelegram(mensagem string) error {
	// Carregar o arquivo .env
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Erro ao carregar arquivo .env")
	}

	telegramToken := os.Getenv("TELEGRAM_TOKEN")
	telegramChatID := os.Getenv("TELEGRAM_CHAT_ID")

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", telegramToken)

	body := map[string]interface{}{
		"chat_id": telegramChatID,
		"text":    mensagem,
	}

	jsonBody, _ := json.Marshal(body)

	resp, err := http.Post(apiURL, "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

func main() {
	sites := lerSitesDoArquivo()

	for _, url := range sites {
		verificaSite(url)
	}
	exibirEstatisticas()

	sitesDown := obterSitesForaDoAr()
	if len(sitesDown) > 0 {
		message := formatarMensagemAlerta(sitesDown)
		err := enviarAlertaTelegram(message)
		if err != nil {
			log.Fatal(err)
		}
	}
}
