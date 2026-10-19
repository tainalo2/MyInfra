import type { CatalogueCard, CatalogueInput } from "./api";
import { categoryLabel, currentLang, text, type Lang } from "./i18n";

type Line = { summary: string; description: string };

const copy: Record<string, Partial<Record<Lang, Line>>> = {
  wordpress: L(
    ["A site and its writing space.", "A site of pages and posts, with its writing space. Texts, media, and the database are kept."],
    ["Um site e o seu espaço de escrita.", "Um site de páginas e artigos, com o seu espaço de escrita. Os textos, os média e a base são conservados."],
    ["Un sito e il suo spazio di scrittura.", "Un sito di pagine e articoli, con il suo spazio di scrittura. Testi, media e database sono conservati."],
    ["Eine Website und ihr Schreibraum.", "Eine Website aus Seiten und Beiträgen, mit ihrem Schreibraum. Texte, Medien und die Datenbank bleiben erhalten."],
  ),
  ghost: L(
    ["An online publication.", "An online publication, built for posts and subscribers. Texts and the database are kept."],
    ["Uma publicação em linha.", "Uma publicação em linha, pensada para artigos e subscritores. Os textos e a base são conservados."],
    ["Una pubblicazione in linea.", "Una pubblicazione in linea, pensata per articoli e abbonati. Testi e database sono conservati."],
    ["Eine Online-Publikation.", "Eine Online-Publikation für Beiträge und Abonnenten. Texte und die Datenbank bleiben erhalten."],
  ),
  wikijs: L(
    ["A shared wiki.", "A wiki for writing and linking pages together. The content is stored in the database."],
    ["Um wiki partilhado.", "Um wiki para escrever e ligar páginas a várias pessoas. O conteúdo fica na base."],
    ["Un wiki condiviso.", "Un wiki per scrivere e collegare pagine in più persone. Il contenuto sta nel database."],
    ["Ein gemeinsames Wiki.", "Ein Wiki, um Seiten zu schreiben und zu verknüpfen. Der Inhalt liegt in der Datenbank."],
  ),
  bookstack: L(
    ["Notes arranged on shelves.", "Notes organised as shelves, books, and chapters. Pages and attachments are kept."],
    ["Notas arrumadas em estantes.", "Notas organizadas em estantes, livros e capítulos. As páginas e os anexos são conservados."],
    ["Note ordinate in scaffali.", "Note organizzate in scaffali, libri e capitoli. Pagine e allegati sono conservati."],
    ["Notizen in Regalen.", "Notizen in Regalen, Büchern und Kapiteln. Seiten und Anhänge bleiben erhalten."],
  ),
  dokuwiki: L(
    ["A file wiki, with no separate database.", "A wiki where each page is a file, with no separate database. Pages stay on disk."],
    ["Um wiki de ficheiros, sem base à parte.", "Um wiki em que cada página é um ficheiro, sem base à parte. As páginas ficam no disco."],
    ["Un wiki di file, senza database a parte.", "Un wiki in cui ogni pagina è un file, senza database a parte. Le pagine restano sul disco."],
    ["Ein Datei-Wiki, ohne eigene Datenbank.", "Ein Wiki, in dem jede Seite eine Datei ist, ohne eigene Datenbank. Die Seiten bleiben auf der Platte."],
  ),
  grav: L(
    ["A site of pages as files.", "A site made of files: pages, themes, and media. Nothing is stored in a database."],
    ["Um site de páginas em ficheiros.", "Um site feito de ficheiros: páginas, temas e média. Nada fica numa base."],
    ["Un sito di pagine in file.", "Un sito fatto di file: pagine, temi e media. Niente è affidato a un database."],
    ["Eine Website aus Dateien.", "Eine Website aus Dateien: Seiten, Themen und Medien. Nichts liegt in einer Datenbank."],
  ),
  freshrss: L(
    ["A feed reader.", "A reader for following feeds and reading articles in one place. Subscriptions and history are kept."],
    ["Um leitor de feeds.", "Um leitor para seguir feeds e ler artigos no mesmo sítio. As subscrições e o histórico são conservados."],
    ["Un lettore di feed.", "Un lettore per seguire i feed e leggere gli articoli nello stesso posto. Abbonamenti e cronologia sono conservati."],
    ["Ein Feed-Leser.", "Ein Leser für Feeds und Artikel an einem Ort. Abonnements und Verlauf bleiben erhalten."],
  ),
  miniflux: L(
    ["A plain feed reader.", "A deliberately simple feed reader. Subscriptions and read articles are in the database."],
    ["Um leitor de feeds despido.", "Um leitor de feeds deliberadamente simples. As subscrições e os artigos lidos estão na base."],
    ["Un lettore di feed essenziale.", "Un lettore di feed volutamente semplice. Abbonamenti e articoli letti stanno nel database."],
    ["Ein schlichter Feed-Leser.", "Ein bewusst einfacher Feed-Leser. Abonnements und gelesene Artikel liegen in der Datenbank."],
  ),
  linkding: L(
    ["Saved links.", "A collection of links, with tags and search. Saved links are kept."],
    ["Ligações guardadas.", "Uma coleção de ligações, com etiquetas e pesquisa. As ligações guardadas são conservadas."],
    ["Link salvati.", "Una raccolta di link, con etichette e ricerca. I link salvati sono conservati."],
    ["Gespeicherte Links.", "Eine Sammlung von Links, mit Schlagwörtern und Suche. Gespeicherte Links bleiben erhalten."],
  ),
  wallabag: L(
    ["Pages set aside for later.", "Pages set aside to read later, without their original layout. Saved articles are kept."],
    ["Páginas postas de lado para mais tarde.", "As páginas que se põem de lado para ler mais tarde, sem a formatação de origem. Os artigos guardados são conservados."],
    ["Pagine messe da parte per dopo.", "Le pagine messe da parte per leggerle dopo, senza l'impaginazione originale. Gli articoli salvati sono conservati."],
    ["Seiten für später.", "Seiten, die man für später zurücklegt, ohne das ursprüngliche Layout. Gespeicherte Artikel bleiben erhalten."],
  ),
  shlink: L(
    ["Short links.", "Short addresses that point to the original links, with a visit count. The links are in the database."],
    ["Ligações curtas.", "Endereços curtos que apontam para as ligações de origem, com o seguimento das visitas. As ligações estão na base."],
    ["Link brevi.", "Indirizzi brevi che rimandano ai link originali, con il conteggio delle visite. I link stanno nel database."],
    ["Kurze Links.", "Kurze Adressen, die auf die ursprünglichen Links zeigen, mit Besuchszählung. Die Links liegen in der Datenbank."],
  ),
  umami: L(
    ["A quiet audience measure.", "A light audience measure: page views and origins, without an advertising profile. Visits are in the database."],
    ["Uma medida de audiência sóbria.", "Uma medida de audiência leve: páginas vistas e origens, sem perfil publicitário. As visitas estão na base."],
    ["Una misura d'audience sobria.", "Una misura d'audience leggera: pagine viste e origini, senza profilo pubblicitario. Le visite stanno nel database."],
    ["Eine nüchterne Reichweitenmessung.", "Eine leichte Reichweitenmessung: Seitenaufrufe und Herkunft, ohne Werbeprofil. Die Besuche liegen in der Datenbank."],
  ),
  matomo: L(
    ["A full audience measure.", "A full audience measure: pages, campaigns, and behaviour. Statistics are kept."],
    ["Uma medida de audiência completa.", "Uma medida de audiência completa: páginas, campanhas e comportamento. As estatísticas são conservadas."],
    ["Una misura d'audience completa.", "Una misura d'audience completa: pagine, campagne e comportamento. Le statistiche sono conservate."],
    ["Eine vollständige Reichweitenmessung.", "Eine vollständige Reichweitenmessung: Seiten, Kampagnen und Verhalten. Die Statistiken bleiben erhalten."],
  ),
  nextcloud: L(
    ["Shared files, calendar, and contacts.", "Files, calendar, contacts, and sharing, for one person or a team. Files and the database are kept."],
    ["Ficheiros, agenda e contactos partilhados.", "Ficheiros, agenda, contactos e partilha, para uma pessoa ou uma equipa. Os ficheiros e a base são conservados."],
    ["File, agenda e contatti condivisi.", "File, agenda, contatti e condivisione, per una persona o un gruppo. File e database sono conservati."],
    ["Geteilte Dateien, Kalender und Kontakte.", "Dateien, Kalender, Kontakte und Freigabe, für eine Person oder ein Team. Dateien und die Datenbank bleiben erhalten."],
  ),
  cryptpad: L(
    ["Documents encrypted in the browser.", "Documents, spreadsheets, and presentations encrypted in the browser. The pads are kept."],
    ["Documentos cifrados no navegador.", "Documentos, folhas e apresentações cifrados no navegador. Os pads são conservados."],
    ["Documenti cifrati nel browser.", "Documenti, fogli e presentazioni cifrati nel browser. I pad sono conservati."],
    ["Im Browser verschlüsselte Dokumente.", "Dokumente, Tabellen und Präsentationen, im Browser verschlüsselt. Die Pads bleiben erhalten."],
  ),
  mattermost: L(
    ["A team conversation.", "Team conversations in channels, with history and attachments. Messages are in the database."],
    ["Uma conversa de equipa.", "Conversas de equipa por salas, com o histórico e os ficheiros anexos. As mensagens estão na base."],
    ["Una conversazione di squadra.", "Conversazioni di squadra per stanze, con cronologia e allegati. I messaggi stanno nel database."],
    ["Ein Teamgespräch.", "Teamgespräche in Kanälen, mit Verlauf und Anhängen. Die Nachrichten liegen in der Datenbank."],
  ),
  vikunja: L(
    ["Task lists.", "Lists and tasks, with due dates and sharing. The tasks are kept."],
    ["Listas de tarefas.", "Listas e tarefas, com prazos e partilha. As tarefas são conservadas."],
    ["Liste di attività.", "Liste e attività, con scadenze e condivisione. Le attività sono conservate."],
    ["Aufgabenlisten.", "Listen und Aufgaben, mit Fristen und Freigabe. Die Aufgaben bleiben erhalten."],
  ),
  nocodb: L(
    ["A database as a table.", "A database shown as a table, which can be linked and shared. The data is in the database."],
    ["Uma base em forma de tabela.", "Uma base de dados apresentada como uma tabela, que se pode ligar e partilhar. Os dados estão na base."],
    ["Un database in forma di tabella.", "Un database presentato come una tabella, da collegare e condividere. I dati stanno nel database."],
    ["Eine Datenbank als Tabelle.", "Eine Datenbank als Tabelle, die man verknüpfen und teilen kann. Die Daten liegen in der Datenbank."],
  ),
  baserow: L(
    ["A shared spreadsheet.", "A shared spreadsheet, where each row is a record. The tables are kept."],
    ["Uma folha de cálculo partilhada.", "Uma folha de cálculo partilhada, em que cada linha é um dado. As tabelas são conservadas."],
    ["Un foglio condiviso.", "Un foglio condiviso, in cui ogni riga è un dato. Le tabelle sono conservate."],
    ["Eine geteilte Tabelle.", "Eine geteilte Tabelle, in der jede Zeile ein Datensatz ist. Die Tabellen bleiben erhalten."],
  ),
  memos: L(
    ["Short notes.", "Short, dated notes, to keep a trace through the days. The notes are kept."],
    ["Notas curtas.", "Notas curtas, datadas, para guardar um rasto ao longo dos dias. As notas são conservadas."],
    ["Note brevi.", "Note brevi, datate, per tenere traccia dei giorni. Le note sono conservate."],
    ["Kurze Notizen.", "Kurze, datierte Notizen, um die Tage festzuhalten. Die Notizen bleiben erhalten."],
  ),
  trilium: L(
    ["Notes linked together.", "Notes linked together, with a tree and links. The notebook is kept."],
    ["Notas ligadas entre si.", "Notas ligadas entre si, com uma árvore e ligações. O caderno é conservado."],
    ["Note collegate tra loro.", "Note collegate tra loro, con un albero e dei link. Il quaderno è conservato."],
    ["Miteinander verbundene Notizen.", "Notizen, die miteinander verbunden sind, mit einem Baum und Links. Das Notizbuch bleibt erhalten."],
  ),
  affine: L(
    ["Notes, tables, and drawings in one place.", "Notes, tables, and drawings in the same workspace. The content is kept in the database."],
    ["Notas, tabelas e desenhos no mesmo sítio.", "Notas, tabelas e desenhos reunidos no mesmo espaço de trabalho. O conteúdo é conservado na base."],
    ["Note, tabelle e disegni nello stesso posto.", "Note, tabelle e disegni nello stesso spazio di lavoro. Il contenuto è conservato nel database."],
    ["Notizen, Tabellen und Zeichnungen an einem Ort.", "Notizen, Tabellen und Zeichnungen im selben Arbeitsraum. Der Inhalt bleibt in der Datenbank."],
  ),
  docmost: L(
    ["A space of team pages.", "Team pages, organised in spaces, to document a project. The pages are in the database."],
    ["Um espaço de páginas de equipa.", "Páginas de equipa, organizadas em espaços, para documentar um projeto. As páginas estão na base."],
    ["Uno spazio di pagine di squadra.", "Pagine di squadra, organizzate in spazi, per documentare un progetto. Le pagine stanno nel database."],
    ["Ein Raum für Teamseiten.", "Teamseiten, in Räumen geordnet, um ein Projekt zu dokumentieren. Die Seiten liegen in der Datenbank."],
  ),
  outline: L(
    ["A knowledge base.", "A knowledge base: collections, pages, and search. The documents are kept."],
    ["Uma base de conhecimentos.", "Uma base de conhecimentos: coleções, páginas e pesquisa. Os documentos são conservados."],
    ["Una base di conoscenza.", "Una base di conoscenza: raccolte, pagine e ricerca. I documenti sono conservati."],
    ["Eine Wissensbasis.", "Eine Wissensbasis: Sammlungen, Seiten und Suche. Die Dokumente bleiben erhalten."],
  ),
  wekan: L(
    ["A board of cards.", "A board of cards to follow work from one column to the next. The cards are in the database."],
    ["Um quadro de cartões.", "Um quadro de cartões para seguir um trabalho de uma coluna à seguinte. Os cartões estão na base."],
    ["Una bacheca di schede.", "Una bacheca di schede per seguire un lavoro da una colonna all'altra. Le schede stanno nel database."],
    ["Eine Tafel aus Karten.", "Eine Tafel aus Karten, um Arbeit von einer Spalte zur nächsten zu verfolgen. Die Karten liegen in der Datenbank."],
  ),
  excalidraw: L(
    ["A whiteboard.", "A whiteboard for drawing a diagram by hand, alone or together. This install does not keep the drawings."],
    ["Um quadro branco.", "Um quadro branco para desenhar um esquema à mão, sozinho ou a várias pessoas. Esta instalação não guarda os desenhos."],
    ["Una lavagna.", "Una lavagna per disegnare uno schema a mano, da soli o in più persone. Questa installazione non conserva i disegni."],
    ["Ein Whiteboard.", "Ein Whiteboard, um ein Schema von Hand zu zeichnen, allein oder gemeinsam. Diese Installation behält die Zeichnungen nicht."],
  ),
  etherpad: L(
    ["A text written by several people.", "A text written by several people at once, with the history of changes. The pads are kept."],
    ["Um texto escrito a várias pessoas.", "Um texto escrito a várias pessoas ao mesmo tempo, com o histórico das alterações. Os pads são conservados."],
    ["Un testo scritto in più persone.", "Un testo scritto in più persone nello stesso momento, con la cronologia delle modifiche. I pad sono conservati."],
    ["Ein Text, den mehrere schreiben.", "Ein Text, den mehrere gleichzeitig schreiben, mit der Änderungshistorie. Die Pads bleiben erhalten."],
  ),
  hedgedoc: L(
    ["Notes in Markdown.", "Notes in Markdown, published or kept for yourself. The notes are in the database."],
    ["Notas em markdown.", "Notas em markdown, publicadas ou guardadas para si. As notas estão na base."],
    ["Note in markdown.", "Note in markdown, pubblicate o tenute per sé. Le note stanno nel database."],
    ["Notizen in Markdown.", "Notizen in Markdown, veröffentlicht oder für sich behalten. Die Notizen liegen in der Datenbank."],
  ),
  penpot: L(
    ["A workshop for mockups.", "A workshop for drawing screen mockups and components. The working files are kept."],
    ["Uma oficina de maquetas.", "Uma oficina para desenhar maquetas de ecrãs e componentes. Os ficheiros de trabalho são conservados."],
    ["Un laboratorio di bozze.", "Un laboratorio per disegnare bozze di schermate e componenti. I file di lavoro sono conservati."],
    ["Eine Werkstatt für Entwürfe.", "Eine Werkstatt, um Bildschirmentwürfe und Komponenten zu zeichnen. Die Arbeitsdateien bleiben erhalten."],
  ),
  calcom: L(
    ["Appointments to book.", "A page where others book a meeting according to your availability. Bookings are in the database."],
    ["Encontros a reservar.", "Uma página onde os outros reservam um encontro segundo as suas disponibilidades. As reservas estão na base."],
    ["Appuntamenti da prenotare.", "Una pagina dove gli altri prenotano un appuntamento secondo le tue disponibilità. Le prenotazioni stanno nel database."],
    ["Termine zum Buchen.", "Eine Seite, auf der andere einen Termin nach Ihrer Verfügbarkeit buchen. Die Buchungen liegen in der Datenbank."],
  ),
  forgejo: L(
    ["Git repositories and their requests.", "Git repositories, change requests, and issue tracking. The repositories are kept."],
    ["Repositórios Git e os seus pedidos.", "Repositórios Git, pedidos de alteração e seguimento de tickets. Os repositórios são conservados."],
    ["Repository Git e le loro richieste.", "Repository Git, richieste di modifica e ticket. I repository sono conservati."],
    ["Git-Depots und ihre Anfragen.", "Git-Depots, Änderungsanfragen und Tickets. Die Depots bleiben erhalten."],
  ),
  "code-server": L(
    ["An editor in the browser.", "A code editor opened in the browser, on the machine's files. The working folder is kept."],
    ["Um editor no navegador.", "Um editor de código aberto no navegador, sobre os ficheiros da máquina. A pasta de trabalho é conservada."],
    ["Un editor nel browser.", "Un editor di codice aperto nel browser, sui file della macchina. La cartella di lavoro è conservata."],
    ["Ein Editor im Browser.", "Ein Code-Editor im Browser, auf den Dateien der Maschine. Der Arbeitsordner bleibt erhalten."],
  ),
  verdaccio: L(
    ["A private package registry.", "A private registry for publishing and installing packages. Deposited packages are kept."],
    ["Um registo de pacotes privado.", "Um registo privado para publicar e instalar pacotes. Os pacotes depositados são conservados."],
    ["Un registro di pacchetti privato.", "Un registro privato per pubblicare e installare pacchetti. I pacchetti depositati sono conservati."],
    ["Eine private Paketregistratur.", "Eine private Registratur, um Pakete zu veröffentlichen und zu installieren. Abgelegte Pakete bleiben erhalten."],
  ),
  meilisearch: L(
    ["A search engine.", "A fast search engine that other applications can query. The index is kept."],
    ["Um motor de pesquisa.", "Um motor de pesquisa rápido, que outras aplicações podem consultar. O índice é conservado."],
    ["Un motore di ricerca.", "Un motore di ricerca veloce, che altre applicazioni possono interrogare. L'indice è conservato."],
    ["Eine Suchmaschine.", "Eine schnelle Suchmaschine, die andere Anwendungen abfragen können. Der Index bleibt erhalten."],
  ),
  typesense: L(
    ["A search engine.", "A search engine to query from other applications. The index is kept."],
    ["Um motor de pesquisa.", "Um motor de pesquisa a consultar a partir de outras aplicações. O índice é conservado."],
    ["Un motore di ricerca.", "Un motore di ricerca da interrogare da altre applicazioni. L'indice è conservato."],
    ["Eine Suchmaschine.", "Eine Suchmaschine, die andere Anwendungen abfragen. Der Index bleibt erhalten."],
  ),
  pocketbase: L(
    ["A small database and its interface.", "A small database, its admin interface, and an API. The data is kept."],
    ["Uma pequena base e a sua interface.", "Uma pequena base, a sua interface de administração e uma API. Os dados são conservados."],
    ["Un piccolo database e la sua interfaccia.", "Un piccolo database, la sua interfaccia di amministrazione e un'API. I dati sono conservati."],
    ["Eine kleine Datenbank und ihre Oberfläche.", "Eine kleine Datenbank, ihre Verwaltungsoberfläche und eine API. Die Daten bleiben erhalten."],
  ),
  registry: L(
    ["A store of container images.", "A store where application images are sent and pulled. Deposited images are kept."],
    ["Um depósito de imagens de contentores.", "Um depósito onde se enviam e se recuperam imagens de aplicações. As imagens depositadas são conservadas."],
    ["Un deposito di immagini di container.", "Un deposito dove inviare e riprendere immagini di applicazioni. Le immagini depositate sono conservate."],
    ["Ein Lager für Container-Images.", "Ein Lager, in das Anwendungs-Images geschickt und wieder geholt werden. Abgelegte Images bleiben erhalten."],
  ),
  vaultwarden: L(
    ["A password vault.", "A password vault, compatible with Bitwarden applications. The vault is kept."],
    ["Um cofre de palavras-passe.", "Um cofre de palavras-passe, compatível com as aplicações Bitwarden. O cofre é conservado."],
    ["Una cassaforte di password.", "Una cassaforte di password, compatibile con le applicazioni Bitwarden. La cassaforte è conservata."],
    ["Ein Passworttresor.", "Ein Passworttresor, kompatibel mit den Bitwarden-Anwendungen. Der Tresor bleibt erhalten."],
  ),
  authentik: L(
    ["An identity door for the other services.", "An identity door: accounts, groups, and sign-in for the other services. Accounts are in the database."],
    ["Uma porta de identidade para os outros serviços.", "Uma porta de identidade: contas, grupos e entrada para os outros serviços. As contas estão na base."],
    ["Una porta d'identità per gli altri servizi.", "Una porta d'identità: account, gruppi e accesso per gli altri servizi. Gli account stanno nel database."],
    ["Eine Identitätstür für die anderen Dienste.", "Eine Identitätstür: Konten, Gruppen und Anmeldung für die anderen Dienste. Die Konten liegen in der Datenbank."],
  ),
  keycloak: L(
    ["An identity service.", "An identity service for connecting applications with the same accounts. Accounts are in the database."],
    ["Um serviço de identidade.", "Um serviço de identidade para ligar aplicações com as mesmas contas. As contas estão na base."],
    ["Un servizio d'identità.", "Un servizio d'identità per collegare applicazioni con gli stessi account. Gli account stanno nel database."],
    ["Ein Identitätsdienst.", "Ein Identitätsdienst, um Anwendungen mit denselben Konten zu verbinden. Die Konten liegen in der Datenbank."],
  ),
  lldap: L(
    ["A light directory and its screen.", "A light directory and its screen, to manage users and groups. The directory is kept."],
    ["Um diretório leve e o seu ecrã.", "Um diretório leve e o seu ecrã, para gerir utilizadores e grupos. O diretório é conservado."],
    ["Una rubrica leggera e la sua schermata.", "Una rubrica leggera e la sua schermata, per gestire utenti e gruppi. La rubrica è conservata."],
    ["Ein leichtes Verzeichnis und sein Bildschirm.", "Ein leichtes Verzeichnis und sein Bildschirm, um Benutzer und Gruppen zu verwalten. Das Verzeichnis bleibt erhalten."],
  ),
  filebrowser: L(
    ["The files of a folder, in the browser.", "The files of a folder, browsed and uploaded from the browser. The folder is kept."],
    ["Os ficheiros de uma pasta, no navegador.", "Os ficheiros de uma pasta, percorridos e depositados a partir do navegador. A pasta é conservada."],
    ["I file di una cartella, nel browser.", "I file di una cartella, percorsi e depositati dal browser. La cartella è conservata."],
    ["Die Dateien eines Ordners, im Browser.", "Die Dateien eines Ordners, durchsucht und abgelegt aus dem Browser. Der Ordner bleibt erhalten."],
  ),
  directus: L(
    ["A content database.", "A content database with an admin screen and an API. Content and files are kept."],
    ["Uma base de conteúdo.", "Uma base de conteúdo com um ecrã de administração e uma API. O conteúdo e os ficheiros são conservados."],
    ["Un database di contenuti.", "Un database di contenuti con una schermata di amministrazione e un'API. Contenuti e file sono conservati."],
    ["Eine Inhaltsdatenbank.", "Eine Inhaltsdatenbank mit einem Verwaltungsbildschirm und einer API. Inhalt und Dateien bleiben erhalten."],
  ),
  metabase: L(
    ["Questions about a database.", "Questions and charts asked of a database. Saved questions are in the database."],
    ["Perguntas sobre uma base de dados.", "Perguntas e gráficos feitos sobre uma base de dados. As perguntas guardadas estão na base."],
    ["Domande su un database.", "Domande e grafici posti su un database. Le domande salvate stanno nel database."],
    ["Fragen an eine Datenbank.", "Fragen und Diagramme an eine Datenbank. Gespeicherte Fragen liegen in der Datenbank."],
  ),
  grafana: L(
    ["Dashboards.", "Dashboards for following measurements over time. The dashboards are kept."],
    ["Painéis.", "Painéis para seguir medições ao longo do tempo. Os painéis são conservados."],
    ["Cruscotti.", "Cruscotti per seguire le misure nel tempo. I cruscotti sono conservati."],
    ["Übersichten.", "Übersichten, um Messwerte über die Zeit zu verfolgen. Die Übersichten bleiben erhalten."],
  ),
  appsmith: L(
    ["Screens laid over data.", "Screens composed over existing data. The applications you create are kept."],
    ["Ecrãs pousados sobre dados.", "Ecrãs que se compõem por cima de dados existentes. As aplicações criadas são conservadas."],
    ["Schermate posate sui dati.", "Schermate composte sopra dati esistenti. Le applicazioni create sono conservate."],
    ["Bildschirme über Daten.", "Bildschirme, die man über vorhandene Daten legt. Die erstellten Anwendungen bleiben erhalten."],
  ),
  n8n: L(
    ["Automations between services.", "Automations that connect services, step by step. The scenarios are kept."],
    ["Automatizações entre serviços.", "Automatizações que ligam serviços entre si, passo a passo. Os cenários são conservados."],
    ["Automazioni tra servizi.", "Automazioni che collegano servizi, passo dopo passo. Gli scenari sono conservati."],
    ["Automatisierungen zwischen Diensten.", "Automatisierungen, die Dienste Schritt für Schritt verbinden. Die Abläufe bleiben erhalten."],
  ),
  activepieces: L(
    ["Automations between services.", "Visual automations between services. The scenarios are in the database."],
    ["Automatizações entre serviços.", "Automatizações visuais entre serviços. Os cenários estão na base."],
    ["Automazioni tra servizi.", "Automazioni visive tra servizi. Gli scenari stanno nel database."],
    ["Automatisierungen zwischen Diensten.", "Visuelle Automatisierungen zwischen Diensten. Die Abläufe liegen in der Datenbank."],
  ),
  "uptime-kuma": L(
    ["Watching sites.", "Watching sites and services, with a status page. The check history is kept."],
    ["A vigilância de sites.", "A vigilância de sites e serviços, com uma página de estado. O histórico dos controlos é conservado."],
    ["La sorveglianza dei siti.", "La sorveglianza di siti e servizi, con una pagina di stato. La cronologia dei controlli è conservata."],
    ["Die Überwachung von Seiten.", "Die Überwachung von Seiten und Diensten, mit einer Statusseite. Die Prüfhistorie bleibt erhalten."],
  ),
  changedetection: L(
    ["An alert when a page changes.", "An alert when a web page changes. Followed pages and the history are kept."],
    ["O alerta quando uma página muda.", "Um alerta quando uma página web muda. As páginas seguidas e o histórico são conservados."],
    ["L'avviso quando una pagina cambia.", "Un avviso quando una pagina web cambia. Le pagine seguite e la cronologia sono conservate."],
    ["Der Alarm, wenn eine Seite sich ändert.", "Ein Alarm, wenn eine Webseite sich ändert. Beobachtete Seiten und der Verlauf bleiben erhalten."],
  ),
  huginn: L(
    ["Agents that watch and act.", "Agents that watch sources and trigger actions. The agents are in the database."],
    ["Agentes que vigiam e agem.", "Agentes que vigiam fontes e disparam ações. Os agentes estão na base."],
    ["Agenti che sorvegliano e agiscono.", "Agenti che sorvegliano fonti e avviano azioni. Gli agenti stanno nel database."],
    ["Agenten, die beobachten und handeln.", "Agenten, die Quellen beobachten und Aktionen auslösen. Die Agenten liegen in der Datenbank."],
  ),
  listmonk: L(
    ["Mailing lists.", "Mailing lists and email campaigns. Subscribers and campaigns are in the database."],
    ["Listas de difusão.", "Listas de difusão e campanhas de e-mail. Os subscritores e as campanhas estão na base."],
    ["Liste di diffusione.", "Liste di diffusione e campagne email. Iscritti e campagne stanno nel database."],
    ["Verteilerlisten.", "Verteilerlisten und E-Mail-Kampagnen. Abonnenten und Kampagnen liegen in der Datenbank."],
  ),
  healthchecks: L(
    ["Confirmation that tasks did run.", "Confirmation that a scheduled task did run, and an alert otherwise. The checks are in the database."],
    ["A confirmação de que as tarefas correram.", "A confirmação de que uma tarefa prevista correu, e um alerta caso contrário. Os controlos estão na base."],
    ["La conferma che i compiti sono andati.", "La conferma che un compito previsto è andato, e un avviso altrimenti. I controlli stanno nel database."],
    ["Die Bestätigung, dass Aufgaben liefen.", "Die Bestätigung, dass eine geplante Aufgabe gelaufen ist, sonst ein Alarm. Die Prüfungen liegen in der Datenbank."],
  ),
  chatwoot: L(
    ["A conversation box with visitors.", "A conversation box for answering a site's visitors. The exchanges are in the database."],
    ["Uma caixa de conversa com os visitantes.", "Uma caixa de conversa para responder aos visitantes de um site. As trocas estão na base."],
    ["Una casella di conversazione con i visitatori.", "Una casella di conversazione per rispondere ai visitatori di un sito. Gli scambi stanno nel database."],
    ["Ein Gesprächskasten mit den Besuchern.", "Ein Gesprächskasten, um den Besuchern einer Seite zu antworten. Die Wechsel liegen in der Datenbank."],
  ),
  freescout: L(
    ["A support mailbox.", "A shared box for answering support emails. Conversations are kept."],
    ["Uma caixa de correio de assistência.", "Uma caixa partilhada para responder aos e-mails de assistência. As conversas são conservadas."],
    ["Una casella di assistenza.", "Una casella condivisa per rispondere alle email di assistenza. Le conversazioni sono conservate."],
    ["Ein Hilfe-Postfach.", "Ein geteiltes Postfach, um Hilfe-E-Mails zu beantworten. Die Gespräche bleiben erhalten."],
  ),
  formbricks: L(
    ["Forms and surveys.", "Forms and surveys, with the answers in one place. The answers are in the database."],
    ["Formulários e inquéritos.", "Formulários e inquéritos, com as respostas no mesmo sítio. As respostas estão na base."],
    ["Moduli e sondaggi.", "Moduli e sondaggi, con le risposte nello stesso posto. Le risposte stanno nel database."],
    ["Formulare und Umfragen.", "Formulare und Umfragen, mit den Antworten an einem Ort. Die Antworten liegen in der Datenbank."],
  ),
  docuseal: L(
    ["Documents to sign.", "Documents to have signed, with the tracking of signatures. The documents are kept."],
    ["Documentos a assinar.", "Documentos a fazer assinar, com o seguimento das assinaturas. Os documentos são conservados."],
    ["Documenti da firmare.", "Documenti da far firmare, con il seguito delle firme. I documenti sono conservati."],
    ["Dokumente zum Unterschreiben.", "Dokumente zum Unterschreiben, mit der Verfolgung der Unterschriften. Die Dokumente bleiben erhalten."],
  ),
  silverbullet: L(
    ["Notes in Markdown.", "Notes in Markdown, linked and searched like a small personal site. The notes are kept."],
    ["Notas em markdown.", "Notas em markdown, ligadas e procuradas como um pequeno site pessoal. As notas são conservadas."],
    ["Note in markdown.", "Note in markdown, collegate e cercate come un piccolo sito personale. Le note sono conservate."],
    ["Notizen in Markdown.", "Notizen in Markdown, verknüpft und durchsucht wie eine kleine persönliche Seite. Die Notizen bleiben erhalten."],
  ),
  joplin: L(
    ["Sync for Joplin notes.", "The server that syncs Joplin notes across devices. The notes are in the database."],
    ["A sincronização das notas Joplin.", "O servidor que sincroniza as notas Joplin entre os aparelhos. As notas estão na base."],
    ["La sincronizzazione delle note Joplin.", "Il server che sincronizza le note Joplin tra i dispositivi. Le note stanno nel database."],
    ["Die Synchronisation der Joplin-Notizen.", "Der Server, der Joplin-Notizen zwischen Geräten synchronisiert. Die Notizen liegen in der Datenbank."],
  ),
  linkwarden: L(
    ["Links sorted and kept.", "Links sorted into collections, with a copy of the page. Links and copies are kept."],
    ["Ligações classificadas e conservadas.", "Ligações classificadas em coleções, com uma cópia da página. As ligações e as cópias são conservadas."],
    ["Link ordinati e conservati.", "Link ordinati in raccolte, con una copia della pagina. Link e copie sono conservati."],
    ["Sortierte und bewahrte Links.", "Links, in Sammlungen geordnet, mit einer Kopie der Seite. Links und Kopien bleiben erhalten."],
  ),
  shiori: L(
    ["Pages set aside.", "Pages set aside to be read again later. Saved articles are kept."],
    ["Páginas postas de lado.", "Páginas postas de lado para serem relidas mais tarde. Os artigos guardados são conservados."],
    ["Pagine messe da parte.", "Pagine messe da parte per essere rilette più tardi. Gli articoli salvati sono conservati."],
    ["Zurückgelegte Seiten.", "Seiten, die man zurücklegt, um sie später wieder zu lesen. Gespeicherte Artikel bleiben erhalten."],
  ),
  firefly: L(
    ["A record of accounts and spending.", "A record of accounts, spending, and budgets. The operations are in the database."],
    ["Um seguimento das contas e das despesas.", "Um seguimento das contas, das despesas e dos orçamentos. As operações estão na base."],
    ["Un seguito di conti e spese.", "Un seguito di conti, spese e budget. Le operazioni stanno nel database."],
    ["Eine Übersicht der Konten und Ausgaben.", "Eine Übersicht der Konten, Ausgaben und Budgets. Die Buchungen liegen in der Datenbank."],
  ),
  actual: L(
    ["A personal budget.", "A personal budget, with the month's accounts and operations. The budget is kept."],
    ["Um orçamento pessoal.", "Um orçamento pessoal, com as contas e as operações do mês. O orçamento é conservado."],
    ["Un budget personale.", "Un budget personale, con i conti e le operazioni del mese. Il budget è conservato."],
    ["Ein persönliches Budget.", "Ein persönliches Budget, mit den Konten und Buchungen des Monats. Das Budget bleibt erhalten."],
  ),
  wallos: L(
    ["Tracking subscriptions.", "Tracking subscriptions: price, due date, and next renewal. The list is kept."],
    ["O seguimento das subscrições.", "O seguimento das subscrições: preço, prazo e próxima renovação. A lista é conservada."],
    ["Il seguito degli abbonamenti.", "Il seguito degli abbonamenti: prezzo, scadenza e prossimo rinnovo. L'elenco è conservato."],
    ["Die Verfolgung der Abos.", "Die Verfolgung der Abos: Preis, Frist und nächste Verlängerung. Die Liste bleibt erhalten."],
  ),
  ghostfolio: L(
    ["A record of wealth.", "A record of wealth: accounts, assets, and change. The data is in the database."],
    ["Um seguimento de património.", "Um seguimento de património: contas, ativos e evolução. Os dados estão na base."],
    ["Un seguito del patrimonio.", "Un seguito del patrimonio: conti, attività ed evoluzione. I dati stanno nel database."],
    ["Eine Vermögensübersicht.", "Eine Vermögensübersicht: Konten, Anlagen und Verlauf. Die Daten liegen in der Datenbank."],
  ),
  invoiceninja: L(
    ["Invoices.", "Quotes, invoices, and payment tracking. The documents are kept."],
    ["Faturas.", "Orçamentos, faturas e o seguimento dos pagamentos. Os documentos são conservados."],
    ["Fatture.", "Preventivi, fatture e il seguito dei pagamenti. I documenti sono conservati."],
    ["Rechnungen.", "Angebote, Rechnungen und die Verfolgung der Zahlungen. Die Dokumente bleiben erhalten."],
  ),
  jellyfin: L(
    ["Films, series, and music.", "Films, series, and music, played in the browser or an application. The configuration and the declared library are kept."],
    ["Filmes, séries e música.", "Filmes, séries e música, lidos no navegador ou numa aplicação. A configuração e a mediateca declarada são conservadas."],
    ["Film, serie e musica.", "Film, serie e musica, letti nel browser o in un'applicazione. La configurazione e la mediateca dichiarata sono conservate."],
    ["Filme, Serien und Musik.", "Filme, Serien und Musik, im Browser oder in einer Anwendung. Die Konfiguration und die erklärte Mediathek bleiben erhalten."],
  ),
  navidrome: L(
    ["A record collection.", "A record collection from the browser and compatible applications. The music and the listening are kept."],
    ["Uma discoteca.", "Uma discoteca acessível a partir do navegador e das aplicações compatíveis. A música e a escuta são conservadas."],
    ["Una discoteca.", "Una discoteca accessibile dal browser e dalle applicazioni compatibili. La musica e l'ascolto sono conservati."],
    ["Eine Plattensammlung.", "Eine Plattensammlung aus dem Browser und kompatiblen Anwendungen. Musik und Hörverlauf bleiben erhalten."],
  ),
  immich: L(
    ["A photo library.", "A private photo library: albums, search, and sharing. Photos and their order are kept."],
    ["Uma fototeca.", "Uma fototeca privada: álbuns, pesquisa e partilha com outras pessoas. As fotos e a sua ordem são conservadas."],
    ["Una fototeca.", "Una fototeca privata: album, ricerca e condivisione. Le foto e il loro ordine sono conservati."],
    ["Eine Fotothek.", "Eine private Fotothek: Alben, Suche und Freigabe. Fotos und ihre Ordnung bleiben erhalten."],
  ),
  photoprism: L(
    ["A photo library.", "A photo library with albums and search. Photos and their index are kept."],
    ["Uma fototeca.", "Uma fototeca com álbuns e pesquisa. As fotos e o seu índice são conservados."],
    ["Una fototeca.", "Una fototeca con album e ricerca. Le foto e il loro indice sono conservati."],
    ["Eine Fotothek.", "Eine Fotothek mit Alben und Suche. Fotos und ihr Index bleiben erhalten."],
  ),
  audiobookshelf: L(
    ["Audiobooks.", "Audiobooks, with reading progress. The books and the progress are kept."],
    ["Livros áudio.", "Livros áudio, com o progresso de leitura. Os livros e o progresso são conservados."],
    ["Audiolibri.", "Audiolibri, con l'avanzamento della lettura. I libri e l'avanzamento sono conservati."],
    ["Hörbücher.", "Hörbücher, mit dem Lesefortschritt. Die Bücher und der Fortschritt bleiben erhalten."],
  ),
  kavita: L(
    ["Comics and books.", "Comics, books, and magazines to read in the browser. The library is kept."],
    ["Bandas desenhadas e livros.", "Bandas desenhadas, livros e revistas para ler no navegador. A biblioteca é conservada."],
    ["Fumetti e libri.", "Fumetti, libri e riviste da leggere nel browser. La biblioteca è conservata."],
    ["Comics und Bücher.", "Comics, Bücher und Zeitschriften zum Lesen im Browser. Die Bibliothek bleibt erhalten."],
  ),
  komga: L(
    ["Comics.", "Comics arranged in series, to read in the browser. The library is kept."],
    ["Bandas desenhadas.", "Bandas desenhadas arrumadas em séries, para ler no navegador. A biblioteca é conservada."],
    ["Fumetti.", "Fumetti ordinati in serie, da leggere nel browser. La biblioteca è conservata."],
    ["Comics.", "Comics, in Serien geordnet, zum Lesen im Browser. Die Bibliothek bleibt erhalten."],
  ),
  "calibre-web": L(
    ["A library of books.", "A library of digital books, to browse and download. The library is kept."],
    ["Uma biblioteca de livros.", "Uma biblioteca de livros digitais, para percorrer e descarregar. A biblioteca é conservada."],
    ["Una biblioteca di libri.", "Una biblioteca di libri digitali, da sfogliare e scaricare. La biblioteca è conservata."],
    ["Eine Bibliothek von Büchern.", "Eine Bibliothek digitaler Bücher, zum Durchsehen und Herunterladen. Die Bibliothek bleibt erhalten."],
  ),
  metube: L(
    ["Downloading videos.", "Downloading videos from an address. Retrieved files are kept."],
    ["O descarregamento de vídeos.", "O descarregamento de vídeos a partir de um endereço. Os ficheiros obtidos são conservados."],
    ["Lo scaricamento di video.", "Lo scaricamento di video a partire da un indirizzo. I file recuperati sono conservati."],
    ["Das Herunterladen von Videos.", "Das Herunterladen von Videos von einer Adresse. Geholte Dateien bleiben erhalten."],
  ),
  pinchflat: L(
    ["Archiving video channels.", "Archiving video channels as new publications arrive. The videos are kept."],
    ["O arquivo de canais de vídeo.", "O arquivo de canais de vídeo, à medida das novas publicações. Os vídeos são conservados."],
    ["L'archivio di canali video.", "L'archivio di canali video, man mano che escono nuove pubblicazioni. I video sono conservati."],
    ["Das Archivieren von Videokanälen.", "Das Archivieren von Videokanälen, sobald neue Veröffentlichungen kommen. Die Videos bleiben erhalten."],
  ),
  homepage: L(
    ["A home page of services.", "A home page that gathers links to services. The layout is kept."],
    ["Uma página de entrada de serviços.", "Uma página de entrada que junta as ligações para os serviços. A disposição é conservada."],
    ["Una pagina iniziale dei servizi.", "Una pagina iniziale che riunisce i link verso i servizi. La disposizione è conservata."],
    ["Eine Startseite der Dienste.", "Eine Startseite, die Links zu den Diensten sammelt. Die Anordnung bleibt erhalten."],
  ),
  dashy: L(
    ["A board of links.", "A board of links to services, organised in sections. The page layout is kept."],
    ["Um quadro de ligações.", "Um quadro de ligações para os serviços, organizado em secções. A disposição da página é conservada."],
    ["Una bacheca di link.", "Una bacheca di link verso i servizi, organizzata in sezioni. La disposizione della pagina è conservata."],
    ["Eine Tafel aus Links.", "Eine Tafel aus Links zu den Diensten, in Abschnitten geordnet. Die Anordnung der Seite bleibt erhalten."],
  ),
  homer: L(
    ["A simple home page.", "A simple home page, made of a list of services. The layout is kept."],
    ["Uma página de entrada simples.", "Uma página de entrada simples, feita de uma lista de serviços. A disposição é conservada."],
    ["Una pagina iniziale semplice.", "Una pagina iniziale semplice, fatta di un elenco di servizi. La disposizione è conservata."],
    ["Eine einfache Startseite.", "Eine einfache Startseite aus einer Liste von Diensten. Die Anordnung bleibt erhalten."],
  ),
  "it-tools": L(
    ["Small tools for technical days.", "Small tools for technical days: conversions, encodings, generators. Nothing is kept."],
    ["Pequenas ferramentas para o dia técnico.", "Pequenas ferramentas para o dia técnico: conversões, codificações, geradores. Nada é conservado."],
    ["Piccoli strumenti per il quotidiano tecnico.", "Piccoli strumenti per il quotidiano tecnico: conversioni, codifiche, generatori. Niente è conservato."],
    ["Kleine Werkzeuge für den technischen Alltag.", "Kleine Werkzeuge für den technischen Alltag: Umwandlungen, Kodierungen, Generatoren. Nichts bleibt erhalten."],
  ),
  "stirling-pdf": L(
    ["Tools for PDFs.", "Tools for handling PDFs: merge, split, convert. The settings are kept."],
    ["Ferramentas para os PDF.", "Ferramentas para manipular PDF: junção, corte, conversão. Os ajustes são conservados."],
    ["Strumenti per i PDF.", "Strumenti per manipolare i PDF: unione, taglio, conversione. Le impostazioni sono conservate."],
    ["Werkzeuge für PDFs.", "Werkzeuge für PDFs: zusammenfügen, schneiden, umwandeln. Die Einstellungen bleiben erhalten."],
  ),
  cyberchef: L(
    ["Transformations of text and data.", "Transformations of text and data, chained in a recipe. Nothing is kept."],
    ["Transformações de texto e de dados.", "Transformações de texto e de dados, encadeadas numa receita. Nada é conservado."],
    ["Trasformazioni di testo e di dati.", "Trasformazioni di testo e di dati, concatenate in una ricetta. Niente è conservato."],
    ["Umwandlungen von Text und Daten.", "Umwandlungen von Text und Daten, in einem Rezept verkettet. Nichts bleibt erhalten."],
  ),
  drawio: L(
    ["Diagrams.", "Diagrams drawn in the browser. This install does not save the drawings to a disk."],
    ["Esquemas.", "Esquemas e diagramas desenhados no navegador. Esta instalação não grava os desenhos num disco."],
    ["Schemi.", "Schemi e diagrammi disegnati nel browser. Questa installazione non registra i disegni su un disco."],
    ["Schemata.", "Schemata und Diagramme, im Browser gezeichnet. Diese Installation speichert die Zeichnungen nicht auf einer Platte."],
  ),
  searxng: L(
    ["A metasearch engine.", "A metasearch engine that queries several searches and opens no visitor account. The configuration is kept."],
    ["Um metamotor de pesquisa.", "Um metamotor que consulta várias pesquisas e não abre conta de visitante. A configuração é conservada."],
    ["Un metamotore di ricerca.", "Un metamotore che interroga più ricerche e non apre un account visitatore. La configurazione è conservata."],
    ["Eine Metasuchmaschine.", "Eine Metasuchmaschine, die mehrere Suchen abfragt und kein Besucher-Konto öffnet. Die Konfiguration bleibt erhalten."],
  ),
  whoogle: L(
    ["A search without an account.", "A search without an account and without a history kept by the service. Nothing is kept from one visit to the next."],
    ["Uma pesquisa sem conta.", "Uma pesquisa sem conta e sem histórico guardado pelo serviço. Nada é conservado de uma visita à seguinte."],
    ["Una ricerca senza account.", "Una ricerca senza account e senza cronologia tenuta dal servizio. Niente è conservato da una visita all'altra."],
    ["Eine Suche ohne Konto.", "Eine Suche ohne Konto und ohne Verlauf, den der Dienst behält. Von einem Besuch zum nächsten bleibt nichts."],
  ),
  gatus: L(
    ["A status page for services.", "A page that says whether services answer, and since when. The configuration is kept."],
    ["Uma página de estado de serviços.", "Uma página que diz se os serviços respondem, e desde quando. A configuração é conservada."],
    ["Una pagina di stato dei servizi.", "Una pagina che dice se i servizi rispondono, e da quando. La configurazione è conservata."],
    ["Eine Statusseite der Dienste.", "Eine Seite, die sagt, ob Dienste antworten, und seit wann. Die Konfiguration bleibt erhalten."],
  ),
  gotify: L(
    ["Notifications to the phone.", "Notifications sent to the phone or the browser. The messages are kept."],
    ["Notificações para o telefone.", "Notificações enviadas para o telefone ou o navegador. As mensagens são conservadas."],
    ["Notifiche verso il telefono.", "Notifiche inviate al telefono o al browser. I messaggi sono conservati."],
    ["Benachrichtigungen ans Telefon.", "Benachrichtigungen an das Telefon oder den Browser. Die Nachrichten bleiben erhalten."],
  ),
  ntfy: L(
    ["Notifications to the phone.", "Notifications published on a topic, received on the phone or the browser. Recent messages are kept."],
    ["Notificações para o telefone.", "Notificações publicadas num assunto, recebidas no telefone ou no navegador. As mensagens recentes são conservadas."],
    ["Notifiche verso il telefono.", "Notifiche pubblicate su un argomento, ricevute sul telefono o nel browser. I messaggi recenti sono conservati."],
    ["Benachrichtigungen ans Telefon.", "Benachrichtigungen, die zu einem Thema veröffentlicht und auf dem Telefon oder im Browser empfangen werden. Die jüngsten Nachrichten bleiben erhalten."],
  ),
  paperless: L(
    ["An archive of documents.", "An archive of documents: drop-off, text recognition, and search. The documents are kept."],
    ["Um arquivo de documentos.", "Um arquivo de documentos: depósito, reconhecimento do texto e pesquisa. Os documentos são conservados."],
    ["Un archivio di documenti.", "Un archivio di documenti: deposito, riconoscimento del testo e ricerca. I documenti sono conservati."],
    ["Ein Archiv von Dokumenten.", "Ein Archiv von Dokumenten: Ablage, Texterkennung und Suche. Die Dokumente bleiben erhalten."],
  ),
};

function L(en: [string, string], pt: [string, string], it: [string, string], de: [string, string]): Partial<Record<Lang, Line>> {
  return {
    en: { summary: en[0], description: en[1] },
    pt: { summary: pt[0], description: pt[1] },
    it: { summary: it[0], description: it[1] },
    de: { summary: de[0], description: de[1] },
  };
}

export function localizeCard(card: CatalogueCard): CatalogueCard {
  const line = copy[card.id]?.[currentLang()];
  return {
    ...card,
    summary: line?.summary ?? card.summary,
    description: line?.description ?? card.description,
    inputs: card.inputs.map(localizeInput),
  };
}

function localizeInput(input: CatalogueInput): CatalogueInput {
  if (input.key === "email") return { ...input, label: text("input.email") };
  return input;
}

export function categoryName(name: string): string {
  return categoryLabel(name);
}

export function cardMatches(card: CatalogueCard, needle: string): boolean {
  if (!needle) return true;
  const local = localizeCard(card);
  const hay = `${card.name} ${card.summary} ${card.description} ${local.summary} ${local.description}`.toLowerCase();
  return hay.includes(needle);
}
