package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type Note struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

// Хранилище заметок в памяти и мьютекс для потокобезопасности
var (
	notes      = make(map[int]Note)
	nextID     = 1
	notesMutex sync.Mutex
)

func main() {
	// CLI-режим
	addCmd := flag.NewFlagSet("add", flag.ExitOnError)
	addTitle := addCmd.String("title", "", "Заголовок заметки")
	addText := addCmd.String("text", "", "Текст заметки")

	deleteCmd := flag.NewFlagSet("delete", flag.ExitOnError)
	deleteID := deleteCmd.Int("id", 0, "ID для удаления")

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "add":
			addCmd.Parse(os.Args[2:])
			if *addTitle == "" || *addText == "" {
				fmt.Println("Нужно указать --title и --text")
				return
			}
			err := loadNotesFromFile("data.json")
			if err != nil {
				fmt.Println("Ошибка загрузки:", err)
				return
			}
			note := Note{
				Title: *addTitle,
				Text:  *addText,
			}
			notesMutex.Lock()
			note.ID = nextID
			nextID++
			notes[note.ID] = note
			notesMutex.Unlock()

			err = saveNotesToFile("data.json")
			if err != nil {
				fmt.Println("Ошибка сохранения:", err)
				return
			}
			fmt.Println("Заметка добавлена с ID:", note.ID)

		case "list":
			err := loadNotesFromFile("data.json")
			if err != nil {
				fmt.Println("Ошибка загрузки:", err)
				return
			}
			for _, note := range notes {
				fmt.Printf("[%d] %s — %s\n", note.ID, note.Title, note.Text)
			}

		case "delete":
			deleteCmd.Parse(os.Args[2:])
			if *deleteID == 0 {
				fmt.Println("Укажи --id для удаления")
				return
			}
			err := loadNotesFromFile("data.json")
			if err != nil {
				fmt.Println("Ошибка загрузки:", err)
				return
			}
			notesMutex.Lock()
			_, exists := notes[*deleteID]
			if exists {
				delete(notes, *deleteID)
				fmt.Println("Заметка удалена.")
			} else {
				fmt.Println("Заметка не найдена.")
			}
			notesMutex.Unlock()
			_ = saveNotesToFile("data.json")

		default:
			fmt.Println("Неизвестная команда:", os.Args[1])
			fmt.Println("Доступные команды: add, list, delete")
		}
		return
	}

	// Если флагов нет — запустить веб-сервер
	err := loadNotesFromFile("data.json")
	if err != nil {
		fmt.Println("Ошибка при загрузке заметок:", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/notes", notesHandler)
	mux.HandleFunc("/notes/", noteByIDHandler)

	fmt.Println("Server started at http://localhost:8080")
	http.ListenAndServe(":8080", mux)
}

func notesHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		getNotes(w, r)
	case http.MethodPost:
		createNote(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func getNotes(w http.ResponseWriter, r *http.Request) {
	notesMutex.Lock()
	defer notesMutex.Unlock()

	// Собираем все ID
	var ids []int
	for id := range notes {
		ids = append(ids, id)
	}
	sort.Ints(ids) // сортируем по возрастанию

	// Собираем отсортированные заметки
	var allNotes []Note
	for _, id := range ids {
		allNotes = append(allNotes, notes[id])
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(allNotes)
}

func createNote(w http.ResponseWriter, r *http.Request) {
	fmt.Println("-> createNote: старт")

	var note Note
	err := json.NewDecoder(r.Body).Decode(&note)
	if err != nil {
		fmt.Println("-> createNote: ошибка при Decode:", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	fmt.Println("-> createNote: распарсили JSON:", note)

	notesMutex.Lock()
	note.ID = nextID
	nextID++
	notes[note.ID] = note
	notesMutex.Unlock()

	err = saveNotesToFile("data.json")
	if err != nil {
		fmt.Println("-> createNote: ошибка при сохранении файла:", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	fmt.Println("-> createNote: успешно добавлена:", note)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(note)
}

func noteByIDHandler(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 3 {
		http.Error(w, "Invalid URL", http.StatusBadRequest)
		return
	}

	idStr := parts[2]
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid note ID", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		notesMutex.Lock()
		note, exists := notes[id]
		notesMutex.Unlock()

		if !exists {
			http.Error(w, "Note not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(note)

	case http.MethodDelete:
		notesMutex.Lock()
		_, exists := notes[id]
		if exists {
			delete(notes, id)
		}
		notesMutex.Unlock()

		if !exists {
			http.Error(w, "Note not found", http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func saveNotesToFile(filename string) error {
	notesMutex.Lock()
	defer notesMutex.Unlock()

	data, err := json.MarshalIndent(notes, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, data, 0644)
}

func loadNotesFromFile(filename string) error {
	notesMutex.Lock()
	defer notesMutex.Unlock()

	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	err = json.Unmarshal(data, &notes)
	if err != nil {
		return err
	}

	maxID := 0
	for id := range notes {
		if id > maxID {
			maxID = id
		}
	}
	nextID = maxID + 1

	fmt.Println("-> loadNotesFromFile: загружено записей:", len(notes))

	return nil
}
