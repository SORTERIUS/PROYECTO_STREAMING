package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"PROYECTO_STREAMING/Backend/database"
	"PROYECTO_STREAMING/Backend/handlers"
	"PROYECTO_STREAMING/Backend/models"

	_ "github.com/go-sql-driver/mysql"
)

// MusicManager es la interfaz principal que define el comportamiento del sistema
type MusicManager interface {
	AddSong(song *models.Song) error
	RemoveSong(id int) error
	PlaySong(id int) error
	PauseSong(id int) error
	GetLibrary() ([]*models.Song, error)
}

// StreamingSystem implementa MusicManager y encapsula la lógica del sistema
type StreamingSystem struct {
	db            *sql.DB
	library       *models.Library
	currentUser   *models.Usuario
	currentPlayer *models.Playback
	mu            sync.RWMutex
}

// NewStreamingSystem crea una nueva instancia del sistema
func NewStreamingSystem(db *sql.DB) (*StreamingSystem, error) {
	library := models.NewLibrary(1) // ID por defecto para pruebas
	return &StreamingSystem{
		db:      db,
		library: library,
	}, nil
}

// Implementación de métodos de la interfaz MusicManager
func (s *StreamingSystem) AddSong(song *models.Song) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := song.ValidateSize(); err != nil {
		return fmt.Errorf("error de validación: %v", err)
	}
	return s.library.AddSong(*song)
}

func (s *StreamingSystem) RemoveSong(id int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.library.RemoveSong(id)
}

func (s *StreamingSystem) PlaySong(id int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	song, err := s.library.GetSongByID(id)
	if err != nil {
		return err
	}

	if s.currentUser == nil {
		return fmt.Errorf("no hay usuario activo")
	}

	s.currentPlayer = models.NewPlayback(s.currentUser.ID, song.ID)
	return s.currentPlayer.Start()
}

func (s *StreamingSystem) PauseSong(id int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.currentPlayer == nil {
		return fmt.Errorf("no hay reproducción activa")
	}
	return s.currentPlayer.Pause()
}

func (s *StreamingSystem) GetLibrary() ([]*models.Song, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	songs := make([]*models.Song, len(s.library.Songs))
	for i := range s.library.Songs {
		songs[i] = &s.library.Songs[i]
	}
	return songs, nil
}

func initializeDatabase(db *sql.DB) error {
	log.Println("Verificando usuarios existentes...")
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		return fmt.Errorf("error verificando usuarios: %v", err)
	}

	log.Printf("Usuarios encontrados: %d", count)

	if count == 0 {
		log.Println("No hay usuarios, procediendo a insertar...")

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("error iniciando transacción: %v", err)
		}

		// Insertar administradores
		adminQuery := `
            INSERT INTO users (name, email, password, role) VALUES
            ('HENRY ALIAGA', 'henry@example.com', 'admin123', 'admin'),
            ('ISMAEL ESPINOZA', 'ismael@example.com', 'admin123', 'admin')
        `
		result, err := tx.Exec(adminQuery)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("error insertando admins: %v", err)
		}
		adminRows, _ := result.RowsAffected()
		log.Printf("Administradores insertados: %d", adminRows)

		// Insertar usuarios regulares
		userQuery := `
            INSERT INTO users (name, email, password, role) VALUES
            ('Juan Perez', 'juan.perez@example.com', 'password123', 'user'),
            ('Ana Gomez', 'ana.gomez@example.com', 'securepass456', 'user'),
            ('Carlos Lopez', 'carlos.lopez@example.com', 'qwerty789', 'user')
        `
		result, err = tx.Exec(userQuery)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("error insertando usuarios: %v", err)
		}
		userRows, _ := result.RowsAffected()
		log.Printf("Usuarios regulares insertados: %d", userRows)

		if err = tx.Commit(); err != nil {
			return fmt.Errorf("error en commit: %v", err)
		}

		log.Println("Transacción completada exitosamente")
	}

	// Verificación final
	var users []struct {
		Email string
		Role  string
	}
	rows, err := db.Query("SELECT email, role FROM users")
	if err != nil {
		return fmt.Errorf("error verificando usuarios finales: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var u struct {
			Email string
			Role  string
		}
		if err := rows.Scan(&u.Email, &u.Role); err != nil {
			return fmt.Errorf("error leyendo usuario: %v", err)
		}
		users = append(users, u)
	}

	log.Printf("Usuarios en la base de datos después de la inicialización:")
	for _, u := range users {
		log.Printf("- Email: %s, Role: %s", u.Email, u.Role)
	}

	return nil
}

// Middleware para verificar autenticación
func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if token == "" {
			http.Error(w, "No autorizado", http.StatusUnauthorized)
			return
		}
		// Validar token o configuración aquí
		next(w, r)
	}
}

// Middleware para verificar el rol de administrador
func adminMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if token != "admin-token" { // Verificación de token de administrador
			http.Error(w, "Acceso no autorizado", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// setupRoutes configura todas las rutas HTTP
func setupRoutes(sys *StreamingSystem) {
	// Configurar manejadores
	userHandler := handlers.NewUserHandler(sys.db)
	authHandler := handlers.NewAuthHandler(sys.db)
	songHandler := handlers.NewSongHandler(sys.db)

	// Servir archivos estáticos del frontend
	fs := http.FileServer(http.Dir("../Frontend"))
	http.Handle("/", http.StripPrefix("/", fs))

	// Rutas de autenticación
	http.HandleFunc("/api/login", authHandler.Login)
	http.HandleFunc("/api/logout", authHandler.Logout)

	// Rutas de usuarios
	http.HandleFunc("/api/users/profile", authMiddleware(userHandler.GetUserProfile))
	http.HandleFunc("/api/users/register", userHandler.Register)

	// Rutas de canciones
	http.HandleFunc("/api/songs", authMiddleware(songHandler.GetSongs))
	http.HandleFunc("/api/songs/add", adminMiddleware(songHandler.AddSong))

	// Rutas de reproducción
	http.HandleFunc("/api/songs/play/", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Método no permitido", http.StatusMethodNotAllowed)
			return
		}

		parts := strings.Split(r.URL.Path, "/")
		if len(parts) < 4 {
			http.Error(w, "ID de canción no proporcionado", http.StatusBadRequest)
			return
		}

		songID, err := strconv.Atoi(parts[len(parts)-1])
		if err != nil {
			http.Error(w, "ID de canción inválido", http.StatusBadRequest)
			return
		}

		if err := sys.PlaySong(songID); err != nil {
			http.Error(w, "Error reproduciendo canción", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))

	http.HandleFunc("/api/songs/pause/", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Método no permitido", http.StatusMethodNotAllowed)
			return
		}

		parts := strings.Split(r.URL.Path, "/")
		if len(parts) < 4 {
			http.Error(w, "ID de canción no proporcionado", http.StatusBadRequest)
			return
		}

		songID, err := strconv.Atoi(parts[len(parts)-1])
		if err != nil {
			http.Error(w, "ID de canción inválido", http.StatusBadRequest)
			return
		}

		if err := sys.PauseSong(songID); err != nil {
			http.Error(w, "Error pausando canción", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))

	// Ruta para búsqueda de canciones
	http.HandleFunc("/api/songs/search", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Método no permitido", http.StatusMethodNotAllowed)
			return
		}

		query := r.URL.Query().Get("q")
		if query == "" {
			http.Error(w, "Parámetro de búsqueda requerido", http.StatusBadRequest)
			return
		}

		results := sys.library.SearchSongs(query)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	}))

	// Rutas para las interfaces de administrador y usuario
	http.HandleFunc("/admin", adminMiddleware(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "../Frontend/admininterface.html")
	}))

	http.HandleFunc("/user", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "../Frontend/userinterface.html")
	}))
}

func main() {
	// Inicializar la base de datos
	config := database.GetDefaultConfig()
	err := database.InitDB(config)
	if err != nil {
		log.Fatalf("Error inicializando base de datos: %v", err)
	}
	defer database.CloseDB()

	// Obtener la conexión a la base de datos
	db := database.GetDB()

	// Inicializar la base de datos con usuarios
	log.Println("Iniciando la inicialización de la base de datos...")
	if err := initializeDatabase(db); err != nil {
		log.Fatalf("Error inicializando datos: %v", err)
	}
	log.Println("Base de datos inicializada correctamente")

	// Crear instancia del sistema
	sys, err := NewStreamingSystem(db)
	if err != nil {
		log.Fatalf("Error creando sistema: %v", err)
	}

	// Configurar rutas
	setupRoutes(sys)

	// Verificar e insertar canciones de ejemplo en la base de datos
	var songCount int
	err = db.QueryRow("SELECT COUNT(*) FROM songs").Scan(&songCount)
	if err != nil {
		log.Printf("Error verificando canciones: %v", err)
	}

	if songCount == 0 {
		log.Println("Cargando canciones de ejemplo...")
		songs := []models.Song{
			{ID: 1, Title: "Thunderstruck", Artist: "AC/DC", Genre: "Rock", FileSize: 5 * 1024 * 1024},
			{ID: 2, Title: "Memories", Artist: "Maroon 5", Genre: "Pop", FileSize: 4 * 1024 * 1024},
			{ID: 3, Title: "Bohemian Rhapsody", Artist: "Queen", Genre: "Rock", FileSize: 6 * 1024 * 1024},
		}

		for _, song := range songs {
			if err := sys.AddSong(&song); err != nil {
				log.Printf("Error agregando canción %s: %v", song.Title, err)
			} else {
				log.Printf("Canción agregada correctamente: %s", song.Title)
			}
		}
		log.Println("Canciones de ejemplo cargadas correctamente")
	}

	// Iniciar servidor HTTP
	port := ":8080"
	log.Printf("Servidor iniciado en http://localhost%s", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("Error iniciando servidor: %v", err)
	}
}