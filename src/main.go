package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Ввод команд пользователем
var reader *bufio.Reader

// Сканер для коирования сайта
var scanner *Scanner

// Инициализация перед запуском
func init() {
	exec.Command("cmd", "/c", "title", APP_NAME).Run()
	exec.Command("cmd", "/c", "mode con cols=220 lines=60").Run()

	reader = bufio.NewReader(os.Stdin)
	scanner = NewScanner()
}

// Точка входа
func main() {
	var params = ScannerParams{
		URL:           "",
		ReplaceOutDir: false,
		RepeatsMax:    10,
	}

START:

	// Запуск:
	cls()
	fmt.Println("Добро пожаловать в программу " + APP_NAME + " v:" + VERSION)

	// Запрос URL:
	params.URL = inputURL("Введите URL сайта для копирования:")

	// Запуск:
	err := scanner.Start(params)
	if err != nil {
		fmt.Println(err)
		if inputYes("Хотите указать другой URL? (y/n)") {
			goto START
		} else {
			return
		}
	}

	// Ожидание результата:
	for {
		switch scanner.State() {
		case ScannerScanning, ScannerPreparing:
		case ScannerReady:
			goto START
		case ScannerComplete:
			goto FINISH
		case ScannerIncorrectURL:
			cls()
			fmt.Println(scanner.Err().Error())
			if inputYes("Хотите указать другой URL? (y/n)") {
				fmt.Println("Операция отменена")
				time.Sleep(time.Second)
				goto START
			} else {
				goto EXIT
			}
		case ScannerOutputDirExist:
			cls()
			if inputYes("Папка с данными для этого сайта уже существует: \"" + scanner.Dir() + "\"\nУдалить старое содержимое? (y/n)") {
				params.ReplaceOutDir = true
				scanner.Start(params)
			} else {
				fmt.Println("Операция отменена")
				time.Sleep(time.Second)
				goto START
			}
		case ScannerOutputDirError:
			cls()
			fmt.Println(scanner.Err().Error())
			if inputYes("Хотите указать другой URL? (y/n)") {
				fmt.Println("Операция отменена")
				time.Sleep(time.Second)
				goto START
			} else {
				goto EXIT
			}
		default:
			cls()
			panic("Я не знаю такого состояния сканера")
		}

		// Вывод информации и ожидание:
		cls()
		fmt.Println(scanner.Report(false))
		time.Sleep(time.Millisecond * 500)
	}

FINISH:

	// Завершено:
	log.Println("\n\nОтчёт сканирования:\n" + scanner.Report(true))

	cls()
	fmt.Println(scanner.Report(true))
	fmt.Println("Сайт скопирован")
	fmt.Println("Нажмите ввод для выхода из программы..")
	reader.ReadRune()

EXIT:

	// Выход из программы:
	fmt.Println("Выход из программы")
	time.Sleep(time.Second)
	return
}

// Получить от пользователя URL сайта для копирования
func inputURL(msg string) string {
	var url string
	var err error
	for {
		fmt.Println(msg)
		url, err = reader.ReadString('\n')
		if err == nil {
			break
		} else {
			fmt.Println("Ошибка чтения ввода:", err)
			time.Sleep(time.Millisecond * 200)
		}
	}
	url = strings.ReplaceAll(url, "\n", "")
	url = strings.ReplaceAll(url, "\r", "")
	return url
}

// Получить от пользователя ввод: y/n
func inputYes(msg string) bool {
	var val string
	var err error
	for {
		fmt.Println(msg)
		val, err = reader.ReadString('\n')
		if err == nil {
			break
		} else {
			fmt.Println("Ошибка чтения ввода:", err)
			time.Sleep(time.Millisecond * 200)
		}
	}

	val = strings.ReplaceAll(val, "\n", "")
	val = strings.ReplaceAll(val, "\r", "")
	val = strings.ToLower(val)

	if len(val) > 0 && val[0] == 'y' {
		return true
	}

	return false
}

// Очистить вывод в консоли
func cls() {
	cmd := exec.Command("cmd", "/c", "cls")
	cmd.Stdout = os.Stdout
	cmd.Run()
}
