package api

import "net/http"

var errorCodes = map[string]string{
	"Requête illisible.":                 "bad_request",
	"Indiquez un nom.":                   "name_required",
	"Ce nom est déjà utilisé.":           "name_taken",
	"La confirmation ne correspond pas.": "confirm_mismatch",
	"Une valeur vide n'est pas enregistrée. Retirez la clé si elle ne sert plus.": "empty_value",
	"Un secret contient au moins une clé.":                                        "secret_needs_key",
	"Un secret contient au plus 50 clés.":                                         "secret_too_many",
	"Une clé ou une valeur n'est pas acceptée.":                                   "secret_bad_entry",
	"Une application indiquée n'existe pas.":                                      "secret_unknown_app",
	"Introuvable.":                                            "not_found",
	"Cet article est introuvable.":                            "article_missing",
	"Une erreur est survenue.":                                "generic",
	"Origine refusée.":                                        "origin",
	"Le mot de passe est déjà choisi.":                        "password_set",
	"Le mot de passe doit contenir entre 8 et 72 caractères.": "password_length",
	"Trop de tentatives. Réessayez dans un instant.":          "too_many",
	"Choisissez d'abord un mot de passe.":                     "password_first",
	"Mot de passe incorrect.":                                 "password_wrong",
	"Connexion requise.":                                      "login_required",
	"Mot de passe actuel incorrect.":                          "password_current",
	"Choisissez un mot de passe différent.":                   "password_same",
	"La clé de développement ne peut pas synchroniser.":       "dev_key",
	"Pair refusé.":                                            "peer_refused",
	"La synchro a échoué.":                                    "sync_failed",
	"Instantané illisible.":                                   "snapshot",
	"Adresse du pair invalide.":                               "peer_url",
	"Cette application ne part pas d'une image.":              "not_image",
	"Une mise à jour est déjà en cours.":                      "rollout_busy",
	"Indiquez chaque image.":                                  "image_each",
	"Indiquez le chemin de l'image, par exemple ghostfolio/ghostfolio:2.168.0.": "image_path",
	"Donnez un nom à l'application.":                                            "app_name",
	"Choisissez une machine ou un cluster.":                                     "pick_destination",
	"Choisissez une machine.":                                                   "pick_machine",
	"Choisissez un cluster.":                                                    "pick_cluster",
	"Dix noms au plus.":                                                         "ten_names",
	"L'adresse de MyInfra se change depuis Transfert.":                          "self_names",
	"Cette machine appartient à un cluster.":                                    "machine_in_cluster",
	"Ce cluster est lu par Argo CD.":                                            "argo_readonly",
	"Ce nom est déjà dans la liste.":                                            "name_listed",
	"Cette version est illisible.":                                              "bad_version",
	"Une variable est illisible.":                                               "bad_variable",
	"Cette destruction n'est pas en échec.":                                     "job_not_failed",
	"Donnez un nom à la machine.":                                               "machine_name",
	"Choisissez un provider.":                                                   "pick_provider",
	"Choisissez au moins une clé SSH.":                                          "pick_ssh",
	"Donnez un nom au cluster.":                                                 "cluster_name",
	"Choisissez une image, un dépôt ou un modèle.":                              "pick_source",
	"Indiquez la source.":                                                       "source_required",
	"Le nombre de copies va de 1 à 10.":                                         "replicas",
	"MyInfra reste en ligne pour piloter le cluster.":                           "self_stays",
	"Un transfert est déjà en cours.":                                           "transfer_busy",
	"Jeton refusé.":                                                             "token_refused",
	"Déjà initialisé.":                                                          "already_setup",
	"Copie illisible.":                                                          "copy_bad",
	"La copie des données a échoué.":                                            "copy_failed",
	"Cette machine porte des applications. Retirez-les avant de la rattacher à un cluster.": "has_apps",
	"Cette machine est dans un cluster. Choisissez le cluster.":                             "in_cluster",
	"Cette machine est déjà reliée.":                                                        "linked",
	"Ce provider a encore des serveurs. Supprimez-les avant.":                               "provider_busy",
	"Ce compte n'est pas un compte DNS.":                                                    "not_dns",
	"Cette machine n'a pas encore d'adresse.":                                               "no_address",
	"Le déploiement est déjà en cours.":                                                     "deploy_busy",
	"Ce type de machine n'est plus proposé à cet endroit.":                                  "offer_place",
	"Ce déploiement n'est plus en cours.":                                                   "deploy_idle",
	"Ce serveur n'est pas lié.":                                                             "server_unlinked",
	"Aucune clé privée pour ouvrir la session SSH.":                                         "no_private_key",
	"Donnez un nom à la clé.":                                                               "key_name",
	"Clé publique illisible.":                                                               "bad_public_key",
	"Cette clé a été importée sans partie privée.":                                          "key_imported",
	"Donnez un nom au jeton.":                                                               "token_name",
	"Type de jeton inconnu.":                                                                "token_kind",
	"Ce compte n'est pas un provider.":                                                      "not_provider",
	"Jeton illisible.":                                                                      "bad_token",
	"Le kubeconfig est illisible.":                                                          "kubeconfig",
	"Ce cluster est livré par MyInfra.":                                                     "cluster_delivered",
	"kubectl est absent du plan de contrôle.":                                               "no_kubectl",
	"Argo CD n'a pas répondu.":                                                              "argo_down",
	"Donnez un nom au template.":                                                            "template_name",
	"Ce cluster est importé.":                                                               "cluster_imported",
	"Ce cluster n'a pas encore de serveur lié.":                                             "cluster_empty",
	"Cette exécution n'est pas en échec.":                                                   "run_not_failed",
	"Les versions n'ont pas pu être lues.":                                                  "versions",
	"Ajoutez un serveur ou une application.":                                                "blueprint_empty",
	"Un cluster nouveau a au moins un serveur.":                                             "blueprint_server",
}

func writeError(w http.ResponseWriter, status int, message string) {
	body := map[string]any{"error": message}
	if code, ok := errorCodes[message]; ok {
		body["code"] = code
	}
	writeJSON(w, status, body)
}

func writeCoded(w http.ResponseWriter, status int, code, fallback string, params map[string]string) {
	body := map[string]any{"error": fallback, "code": code}
	if len(params) > 0 {
		body["params"] = params
	}
	writeJSON(w, status, body)
}
