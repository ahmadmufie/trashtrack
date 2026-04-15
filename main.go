package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	_ "github.com/go-sql-driver/mysql"
)

// Struktur Data Laporan
type Report struct {
	ID        int    `json:"id"`
	Deskripsi string `json:"deskripsi"`
	Lokasi    string `json:"lokasi"`
	FotoURL   string `json:"foto_url"`
	Status    string `json:"status"`
}

var db *sql.DB

func main() {
	// Konfigurasi Database dari Environment Variables
	dbUser := os.Getenv("DB_USER")
	dbPass := os.Getenv("DB_PASSWORD")
	dbHost := os.Getenv("DB_HOST") // Endpoint RDS
	dbName := os.Getenv("DB_NAME")

	dsn := fmt.Sprintf("%s:%s@tcp(%s:3306)/%s", dbUser, dbPass, dbHost, dbName)

	var err error
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		log.Println("Gagal koneksi ke DB (Mungkin sedang testing lokal):", err)
	} else {
		// Buat tabel jika belum ada
		createTable := `CREATE TABLE IF NOT EXISTS laporan (
			id INT AUTO_INCREMENT PRIMARY KEY,
			deskripsi TEXT,
			lokasi VARCHAR(255),
			foto_url TEXT,
			status VARCHAR(50) DEFAULT 'Belum Diangkut'
		);`
		_, err = db.Exec(createTable)
		if err != nil {
			log.Println("Gagal membuat tabel:", err)
		}
	}

	// Routing API & Static File (Frontend)
	http.HandleFunc("/api/reports", handleReports)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	})

	fmt.Println("🚀 TrashTrack Server berjalan di port 8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleReports(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		// 1. Parsing Form Data (Termasuk File)
		err := r.ParseMultipartForm(10 << 20) // Maksimal 10 MB
		if err != nil {
			http.Error(w, "Gagal membaca form", http.StatusBadRequest)
			return
		}

		deskripsi := r.FormValue("deskripsi")
		lokasi := r.FormValue("lokasi")
		file, header, err := r.FormFile("foto")
		if err != nil {
			http.Error(w, "Foto wajib diupload!", http.StatusBadRequest)
			return
		}
		defer file.Close()

		// 2. Upload ke AWS S3
		awsRegion := os.Getenv("AWS_REGION") // Contoh: ap-southeast-1
		bucketName := os.Getenv("S3_BUCKET")

		creds := credentials.NewStaticCredentials(
			os.Getenv("AWS_ACCESS_KEY"),
			os.Getenv("AWS_SECRET_KEY"),
			"",
		)
		sess, err := session.NewSession(&aws.Config{
			Region:      aws.String(awsRegion),
			Credentials: creds,
		})

		uploader := s3manager.NewUploader(sess)
		uploadResult, err := uploader.Upload(&s3manager.UploadInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String("laporan/" + header.Filename),
			Body:   file,
			ACL:    aws.String("public-read"), // Agar foto bisa dilihat publik
		})
		if err != nil {
			http.Error(w, "Gagal upload ke S3: "+err.Error(), http.StatusInternalServerError)
			return
		}

		fotoURL := uploadResult.Location

		// 3. Simpan ke Database RDS
		if db != nil {
			_, err = db.Exec("INSERT INTO laporan (deskripsi, lokasi, foto_url) VALUES (?, ?, ?)", deskripsi, lokasi, fotoURL)
			if err != nil {
				http.Error(w, "Gagal simpan ke DB", http.StatusInternalServerError)
				return
			}
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"message": "Laporan berhasil dikirim!", "foto_url": fotoURL})
		return
	}

	if r.Method == "GET" {
		// 4. Mengambil data dari Database
		rows, err := db.Query("SELECT id, deskripsi, lokasi, foto_url, status FROM laporan ORDER BY id DESC")
		if err != nil {
			http.Error(w, "Gagal mengambil data", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var reports []Report
		for rows.Next() {
			var r Report
			rows.Scan(&r.ID, &r.Deskripsi, &r.Lokasi, &r.FotoURL, &r.Status)
			reports = append(reports, r)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(reports)
	}
}
