package handlers

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"strconv"
)

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func parseInt(s string) (int, error) {
	return strconv.Atoi(s)
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
