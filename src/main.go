package main

import (
	"bufio"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Точка входа.
func main() {
	log.Println("Добро пожаловать в " + APP_NAME + " v:" + VERSION)

	reader := bufio.NewReader(os.Stdin)

	for {
		log.Println("--------------------------")
		log.Println("Введите URL сайта для его копирования")

		// Ввод:
		text, err := reader.ReadString('\n')
		if err != nil {
			log.Println("Ошибка:", err)
			continue
		}
		text = strings.ReplaceAll(text, "\n", "")
		text = strings.ReplaceAll(text, "\r", "")

		// Парсим URL:
		url, err := parseURL(text)
		if err != nil {
			log.Println("Ошибка:", err)
			continue
		}

		// Подготавливаем папку:
		dir, err := filepath.Abs(filepath.Dir(os.Args[0]))
		if err != nil {
			log.Println("Ошибка: Не удалось получить расположение приложения:", err)
			continue
		}
		dir += string(os.PathSeparator) + url.Host

		file, err := os.Stat(dir)
		if err != nil {
			if os.IsNotExist(err) {
				log.Println("Создание папки: \"" + dir + "\"")
				if err = os.Mkdir(dir, 0777); err != nil {
					log.Println("Ошибка: Не удалось создать папку для копии:", err)
					continue
				}
			} else {
				log.Println("Ошибка: Не удалось получить папку для копии:", err)
				continue
			}
		} else {
			if file.IsDir() {
				log.Println("Уже существует, перезаписать? (y/n) \"" + dir + "\"")

				text, err = reader.ReadString('\n')
				if err != nil {
					log.Println("Ошибка: ", err)
					continue
				}
				text = strings.ReplaceAll(text, "\n", "")
				text = strings.ReplaceAll(text, "\r", "")
				text = strings.ToLower(text)
				if len(text) > 0 && text[0] == 'y' {

					// Удаление старой папки:
					log.Println("Очистка папки: \"" + dir + "\"")
					if err = os.RemoveAll(dir); err != nil {
						log.Println("Ошибка: Не удалось удалить старую папку:", err)
						continue
					}

					// Создание новой, пустой:
					if err = os.Mkdir(dir, 0777); err != nil {
						log.Println("Ошибка: Не удалось создать папку для копии:", err)
						continue
					}

				} else {
					log.Println("Отменено")
					continue
				}

			} else {
				log.Println("Ошибка: Нельзя создать папку, занятую файлом: \"" + dir + "\"")
				continue
			}
		}

		// Папка готова для копирования:
		// ...
	}
}

// Разобрать введённый пользователем URL
func parseURL(text string) (*url.URL, error) {
	text = strings.ToLower(text)
	if len(text) < 5 {
		return nil, fmt.Errorf("Слишком короткий адрес сайта")
	}

	// HTTPS:
	if strings.Index(text, "https://") == 0 {
		return url.Parse(text)
	}

	// HTTP:
	if strings.Index(text, "http://") == 0 {
		return url.Parse(text)
	}

	// Относительный:
	if strings.Index(text, "//") == 0 {
		return url.Parse(text)
	}

	// Парсим:
	url, err := url.Parse("http://" + text)
	if err != nil {
		return nil, err
	}

	// Пустой URL:
	if url.Host == "" {
		return nil, fmt.Errorf("Не удалось однозначно прочитать URL")
	}

	return url, nil
}
