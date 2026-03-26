package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	apiGroup   = "une-tasse-de.cafe"
	apiVersion = "v1alpha1"
)

// ---------- types Kubernetes-compatibles ----------

type Cafe struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              CafeSpec   `json:"spec"`
	Status            CafeStatus `json:"status,omitempty"`
}

type CafeSpec struct {
	Type        string `json:"type"`
	Temperature int    `json:"temperature,omitempty"`
}

type CafeStatus struct {
	Ready      bool   `json:"ready,omitempty"`
	LastBrewed string `json:"lastBrewed,omitempty"`
}

type CafeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []Cafe `json:"items"`
}

// ---------- store en mémoire ----------

type Store struct {
	mu    sync.RWMutex
	cafes map[string]map[string]Cafe // namespace -> name -> Cafe
}

func NewStore() *Store {
	s := &Store{cafes: make(map[string]map[string]Cafe)}
	// Quelques cafés de démo pré-chargés
	s.put("default", Cafe{
		TypeMeta:   metav1.TypeMeta{APIVersion: apiGroup + "/" + apiVersion, Kind: "Cafe"},
		ObjectMeta: metav1.ObjectMeta{Name: "espresso-du-matin", Namespace: "default", CreationTimestamp: metav1.Now()},
		Spec:       CafeSpec{Type: "espresso", Temperature: 90},
		Status:     CafeStatus{Ready: true, LastBrewed: time.Now().Format(time.RFC3339)},
	})
	s.put("default", Cafe{
		TypeMeta:   metav1.TypeMeta{APIVersion: apiGroup + "/" + apiVersion, Kind: "Cafe"},
		ObjectMeta: metav1.ObjectMeta{Name: "cappuccino-du-dimanche", Namespace: "default", CreationTimestamp: metav1.Now()},
		Spec:       CafeSpec{Type: "cappuccino", Temperature: 70},
		Status:     CafeStatus{Ready: true, LastBrewed: time.Now().Format(time.RFC3339)},
	})
	return s
}

func (s *Store) put(namespace string, c Cafe) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cafes[namespace] == nil {
		s.cafes[namespace] = make(map[string]Cafe)
	}
	s.cafes[namespace][c.Name] = c
}

func (s *Store) list(namespace string) []Cafe {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var items []Cafe
	if namespace == "" {
		for _, ns := range s.cafes {
			for _, c := range ns {
				items = append(items, c)
			}
		}
	} else {
		for _, c := range s.cafes[namespace] {
			items = append(items, c)
		}
	}
	if items == nil {
		items = []Cafe{}
	}
	return items
}

func (s *Store) get(namespace, name string) (Cafe, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ns, ok := s.cafes[namespace]
	if !ok {
		return Cafe{}, false
	}
	c, ok := ns[name]
	return c, ok
}

func (s *Store) delete(namespace, name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	ns, ok := s.cafes[namespace]
	if !ok {
		return false
	}
	if _, ok = ns[name]; !ok {
		return false
	}
	delete(ns, name)
	return true
}

// ---------- serveur HTTP ----------

type Server struct{ store *Store }

var (
	basePath      = "/apis/" + apiGroup + "/" + apiVersion
	listPattern   = regexp.MustCompile(`^/apis/une-tasse-de\.cafe/v1alpha1/namespaces/([^/]+)/cafes$`)
	brewPattern   = regexp.MustCompile(`^/apis/une-tasse-de\.cafe/v1alpha1/namespaces/([^/]+)/cafes/([^/]+)/brew$`)
	singlePattern = regexp.MustCompile(`^/apis/une-tasse-de\.cafe/v1alpha1/namespaces/([^/]+)/cafes/([^/]+)$`)
)

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Printf("%s %s", r.Method, r.URL.Path)

	switch {
	case r.URL.Path == basePath:
		s.discovery(w, r)
	case r.URL.Path == "/apis/"+apiGroup:
		s.groupDiscovery(w, r)
	case listPattern.MatchString(r.URL.Path):
		m := listPattern.FindStringSubmatch(r.URL.Path)
		s.cafesHandler(w, r, m[1], "")
	case brewPattern.MatchString(r.URL.Path):
		m := brewPattern.FindStringSubmatch(r.URL.Path)
		s.handleBrew(w, r, m[1], m[2])
	case singlePattern.MatchString(r.URL.Path):
		m := singlePattern.FindStringSubmatch(r.URL.Path)
		s.cafesHandler(w, r, m[1], m[2])
	default:
		kubeError(w, http.StatusNotFound, fmt.Sprintf("path %q not found", r.URL.Path))
	}
}

// GET /apis/une-tasse-de.cafe/v1alpha1
// Obligatoire : kube-aggregator appelle ce endpoint pour lister les ressources disponibles.
func (s *Server) discovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, &metav1.APIResourceList{
		TypeMeta:     metav1.TypeMeta{APIVersion: "v1", Kind: "APIResourceList"},
		GroupVersion: apiGroup + "/" + apiVersion,
		APIResources: []metav1.APIResource{
			{
				Name:         "cafes",
				SingularName: "cafe",
				Namespaced:   true,
				Kind:         "Cafe",
				Verbs:        metav1.Verbs{"create", "delete", "get", "list", "update"},
			},
			{
				// Sous-ressource : verbe custom "brew"
				// Accessible via POST .../cafes/{name}/brew
				Name:       "cafes/brew",
				Namespaced: true,
				Kind:       "Cafe",
				Verbs:      metav1.Verbs{"create"},
			},
		},
	})
}

// GET /apis/une-tasse-de.cafe
func (s *Server) groupDiscovery(w http.ResponseWriter, _ *http.Request) {
	gv := apiGroup + "/" + apiVersion
	writeJSON(w, http.StatusOK, &metav1.APIGroup{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "APIGroup"},
		Name:     apiGroup,
		Versions: []metav1.GroupVersionForDiscovery{{GroupVersion: gv, Version: apiVersion}},
		PreferredVersion: metav1.GroupVersionForDiscovery{
			GroupVersion: gv,
			Version:      apiVersion,
		},
	})
}

// Gère les routes /namespaces/{ns}/cafes[/{name}]
func (s *Server) cafesHandler(w http.ResponseWriter, r *http.Request, namespace, name string) {
	if name == "" {
		// Collection : list + create
		switch r.Method {
		case http.MethodGet:
			items := s.store.list(namespace)
			if wantsTable(r) {
				writeJSON(w, http.StatusOK, cafeTable(items...))
				return
			}
			writeJSON(w, http.StatusOK, &CafeList{
				TypeMeta: metav1.TypeMeta{APIVersion: apiGroup + "/" + apiVersion, Kind: "CafeList"},
				Items:    items,
			})
		case http.MethodPost:
			var c Cafe
			if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
				kubeError(w, http.StatusBadRequest, "corps invalide : "+err.Error())
				return
			}
			c.Namespace = namespace
			c.TypeMeta = metav1.TypeMeta{APIVersion: apiGroup + "/" + apiVersion, Kind: "Cafe"}
			if c.CreationTimestamp.IsZero() {
				c.CreationTimestamp = metav1.Now()
			}
			c.Status.LastBrewed = time.Now().Format(time.RFC3339)
			s.store.put(namespace, c)
			writeJSON(w, http.StatusCreated, c)
		default:
			kubeError(w, http.StatusMethodNotAllowed, "méthode non autorisée")
		}
		return
	}

	// Ressource unique : get + update + delete
	switch r.Method {
	case http.MethodGet:
		c, ok := s.store.get(namespace, name)
		if !ok {
			kubeError(w, http.StatusNotFound, fmt.Sprintf(`cafes %q introuvable`, name))
			return
		}
		if wantsTable(r) {
			writeJSON(w, http.StatusOK, cafeTable(c))
			return
		}
		writeJSON(w, http.StatusOK, c)

	case http.MethodPut:
		var c Cafe
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			kubeError(w, http.StatusBadRequest, "corps invalide : "+err.Error())
			return
		}
		c.Namespace = namespace
		c.Name = name
		c.TypeMeta = metav1.TypeMeta{APIVersion: apiGroup + "/" + apiVersion, Kind: "Cafe"}
		s.store.put(namespace, c)
		writeJSON(w, http.StatusOK, c)

	case http.MethodDelete:
		if !s.store.delete(namespace, name) {
			kubeError(w, http.StatusNotFound, fmt.Sprintf(`cafes %q introuvable`, name))
			return
		}
		writeJSON(w, http.StatusOK, &metav1.Status{
			TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"},
			Status:   metav1.StatusSuccess,
			Code:     http.StatusOK,
		})

	default:
		kubeError(w, http.StatusMethodNotAllowed, "méthode non autorisée")
	}
}

// POST /apis/une-tasse-de.cafe/v1alpha1/namespaces/{ns}/cafes/{name}/brew
func (s *Server) handleBrew(w http.ResponseWriter, r *http.Request, namespace, name string) {
	if r.Method != http.MethodPost {
		kubeError(w, http.StatusMethodNotAllowed, "brew n'accepte que POST")
		return
	}
	c, ok := s.store.get(namespace, name)
	if !ok {
		kubeError(w, http.StatusNotFound, fmt.Sprintf(`cafes %q introuvable`, name))
		return
	}
	c.Status.Ready = true
	c.Status.LastBrewed = time.Now().Format(time.RFC3339)
	s.store.put(namespace, c)
	log.Printf("☕ brew: %s/%s (%s, %d°C)", namespace, name, c.Spec.Type, c.Spec.Temperature)
	writeJSON(w, http.StatusOK, c)
}

// ---------- Table API ----------

// wantsTable détecte si kubectl demande un affichage tabulaire.
// kubectl envoie : Accept: application/json;as=Table;v=v1;g=meta.k8s.io
func wantsTable(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "as=Table")
}

// cafeTable construit la réponse metav1.Table avec les colonnes custom.
// C'est l'équivalent de additionalPrinterColumns pour un CRD.
func cafeTable(cafes ...Cafe) *metav1.Table {
	table := &metav1.Table{
		TypeMeta: metav1.TypeMeta{APIVersion: "meta.k8s.io/v1", Kind: "Table"},
		ColumnDefinitions: []metav1.TableColumnDefinition{
			{Name: "Name", Type: "string", Description: "Nom du café"},
			{Name: "Type", Type: "string", Description: "Type de café"},
			{Name: "Temperature", Type: "integer", Description: "Température en °C"},
			{Name: "Ready", Type: "boolean", Description: "Prêt à servir"},
			{Name: "Last Brewed", Type: "string", Description: "Dernière préparation"},
			{Name: "Age", Type: "string", Description: "Âge de la ressource"},
		},
		Rows: []metav1.TableRow{},
	}
	for _, c := range cafes {
		table.Rows = append(table.Rows, metav1.TableRow{
			Cells: []any{
				c.Name,
				c.Spec.Type,
				c.Spec.Temperature,
				c.Status.Ready,
				c.Status.LastBrewed,
				age(c.CreationTimestamp),
			},
		})
	}
	return table
}

// age formate une durée depuis t, comme kubectl (ex: 5m, 2h, 3d).
func age(t metav1.Time) string {
	if t.IsZero() {
		return "<unknown>"
	}
	d := time.Since(t.Time)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// ---------- helpers ----------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func kubeError(w http.ResponseWriter, code int, message string) {
	writeJSON(w, code, &metav1.Status{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"},
		Status:   metav1.StatusFailure,
		Message:  message,
		Code:     int32(code),
	})
}

// ---------- TLS auto-signé (développement) ----------

func generateSelfSignedCert() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "cafe-api-server"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return tls.X509KeyPair(certPEM, keyPEM)
}

// ---------- main ----------

func main() {
	cert, err := generateSelfSignedCert()
	if err != nil {
		log.Fatalf("impossible de générer le certificat TLS : %v", err)
	}

	srv := &http.Server{
		Addr:    ":8443",
		Handler: &Server{store: NewStore()},
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		},
	}

	log.Printf("Cafe API server — %s/%s — écoute sur %s", apiGroup, apiVersion, srv.Addr)
	if err := srv.ListenAndServeTLS("", ""); err != nil {
		log.Fatalf("erreur serveur : %v", err)
	}
}
