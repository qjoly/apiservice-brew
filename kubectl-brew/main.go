package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const usage = `Usage: kubectl brew <cafe-name> [-n namespace]

Prépare un café via l'APIService une-tasse-de.cafe/v1alpha1.

Arguments:
  <cafe-name>          Nom du café à préparer
  -n, --namespace      Namespace Kubernetes (défaut : namespace courant)
  -h, --help           Affiche cette aide

Exemples:
  kubectl brew espresso-du-matin
  kubectl brew cappuccino-du-dimanche -n production
`

type cafeResponse struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		Type        string `json:"type"`
		Temperature int    `json:"temperature"`
	} `json:"spec"`
	Status struct {
		Ready      bool   `json:"ready"`
		LastBrewed string `json:"lastBrewed"`
	} `json:"status"`
}

func main() {
	name, namespace := parseArgs(os.Args[1:])

	if name == "" {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}

	if namespace == "" {
		namespace = currentNamespace()
	}

	path := fmt.Sprintf(
		"/apis/une-tasse-de.cafe/v1alpha1/namespaces/%s/cafes/%s/brew",
		namespace, name,
	)

	fmt.Printf("☕  Brewing %s (namespace: %s)...\n", name, namespace)

	cmd := exec.Command("kubectl", "create", "--raw", path, "-f", "-")
	cmd.Stdin = strings.NewReader("{}")
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erreur : impossible de brewer %q : %v\n", name, err)
		os.Exit(1)
	}

	var cafe cafeResponse
	if err := json.Unmarshal(out, &cafe); err != nil {
		fmt.Fprintf(os.Stderr, "Erreur : réponse inattendue du serveur : %v\n", err)
		os.Exit(1)
	}

	if !cafe.Status.Ready {
		fmt.Fprintf(os.Stderr, "✗  %s n'est pas prêt\n", name)
		os.Exit(1)
	}

	brewed := formatTime(cafe.Status.LastBrewed)
	fmt.Printf("✓  %s est prêt ! (%s, %d°C) — préparé à %s\n",
		name, cafe.Spec.Type, cafe.Spec.Temperature, brewed)
}

func parseArgs(args []string) (name, namespace string) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-h", "--help":
			fmt.Print(usage)
			os.Exit(0)
		case "-n", "--namespace":
			if i+1 < len(args) {
				namespace = args[i+1]
				i++
			}
		default:
			if !strings.HasPrefix(args[i], "-") && name == "" {
				name = args[i]
			}
		}
	}
	return
}

func currentNamespace() string {
	out, err := exec.Command(
		"kubectl", "config", "view",
		"--minify", "-o", "jsonpath={..namespace}",
	).Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return "default"
	}
	return strings.TrimSpace(string(out))
}

func formatTime(raw string) string {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw
	}
	return t.Local().Format("15:04:05 MST")
}
