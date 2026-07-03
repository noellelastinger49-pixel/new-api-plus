# Guide de Configuration du Canal Claude Code

## Présentation

Claude Code est l'outil en ligne de commande officiel d'Anthropic, qui prend en charge l'autorisation d'accès à l'API Claude via OAuth. New API prend en charge Claude Code en tant que type de canal indépendant (Type 200), permettant de réutiliser les identifiants CLI Claude Code de l'utilisateur.

## Fonctionnalités

- **Authentification OAuth** : Utilisation du flux OAuth 2.0 d'Anthropic pour l'autorisation
- **Actualisation automatique** : Actualisation automatique des access tokens expirés
- **Compatibilité des formats d'identifiants** : Prise en charge du format natif CLI Claude Code et du format interne
- **Interface d'autorisation Web** : Flux d'autorisation graphique, sans copier-coller manuel de tokens

## Configuration du Canal

### 1. Création d'un Canal Claude Code

1. Accédez au panneau d'administration → Gestion des canaux → Ajouter un canal
2. Sélectionnez le type de canal : **Claude Code**
3. Renseignez le nom du canal (ex. : `Claude Code - Personnel`)
4. Configurez la clé (deux méthodes)

![1.new_channel.png](1.new_channel.png)

### 2. Méthodes de Configuration des Identifiants

#### Méthode 1 : Autorisation Web (Recommandée)

1. Cliquez sur le bouton « Autoriser »
2. Connectez-vous à votre compte Anthropic dans la fenêtre qui s'ouvre
3. Acceptez l'autorisation d'accès Claude Code
4. Les identifiants seront automatiquement remplis après l'autorisation

![2.gen_credentials.png](2.gen_credentials.png)

#### Méthode 2 : Collage Manuel des Identifiants

Si vous disposez déjà des identifiants CLI Claude Code, vous pouvez coller directement les identifiants au format JSON.

**Format CLI Claude Code** (`~/.claude/config.json`) :

```json
{
  "claudeAiOauth": {
    "accessToken": "sk-ant-oat01-...",
    "refreshToken": "sk-ant-ort01-...",
    "expiresAt": 1782662007674,
    "scopes": ["user:file_upload", "user:inference", "user:mcp_servers", "user:profile", "user:sessions:claude_code"],
    "subscriptionType": "pro",
    "rateLimitTier": "default_claude_ai"
  }
}
```

### 3. Configuration des Modèles

Les modèles pris en charge par le canal Claude Code dépendent du type d'abonnement de votre compte Anthropic.
Modèles courants :
- claude-fable-5
- claude-mythos-5
- claude-sonnet-5
- claude-sonnet-4.6
- claude-sonnet-4.5
- claude-sonnet-4
- claude-sonnet-3.7
- claude-opus-4.8
- claude-opus-4.7
- claude-opus-4.6
- claude-opus-4.5
- claude-haiku-4.5

### 4. Gestion des Identifiants

### Mécanisme d'Actualisation Automatique

New API actualise automatiquement les identifiants Claude Code dans les cas suivants :

1. **Vérification avant requête** : Vérification de l'expiration du token avant chaque requête API (5 minutes à l'avance)
2. **Tâche planifiée** : Analyse régulière de tous les canaux Claude Code par le système pour actualiser les identifiants sur le point d'expirer
3. **Réessai en cas d'échec** : En cas de réponse 401 Unauthorized, tentative automatique d'actualisation suivie d'un réessai

### Actualisation Manuelle

Sur la page de liste des canaux, les canaux Claude Code affichent un bouton « Actualiser les identifiants » permettant de déclencher manuellement l'actualisation.

### Consultation des Identifiants

Les super-administrateurs peuvent consulter les informations complètes des identifiants (y compris le refresh token) sur la page d'édition du canal.

### 5. Test des Modèles

![3.test_models.png](3.test_models.png)

### 6. Chat Instantané

Une fois la configuration terminée, vous pouvez utiliser le canal Claude Code directement dans l'interface de chat intégrée de New API, sans avoir besoin d'un client tiers.

![4.chat.png](4.chat.png)

## Questions Fréquentes

### Q : Que faire si les identifiants expirent ?

R : New API actualise automatiquement les identifiants. En cas d'échec de l'actualisation automatique :
1. Cliquez sur le bouton « Actualiser les identifiants » pour une actualisation manuelle
2. Réautorisez (cliquez sur le bouton « Autoriser »)

### Q : Quels types d'abonnement sont pris en charge ?

R : Tous les types d'abonnement Anthropic sont pris en charge :
- Free (version gratuite)
- Pro (version professionnelle)
- Team (version équipe, accès via identifiants Pro)

Les modèles disponibles dépendent de votre type d'abonnement.

### Q : Comment obtenir les identifiants CLI Claude Code ?

R : Installez et connectez-vous au CLI Claude Code :

```bash
# macOS
brew install claude

# Connexion
claude auth login
```

Après la connexion, les identifiants sont enregistrés dans `~/.claude/config.json`. Sur macOS, ils se trouvent dans le « Trousseau d'accès ».

### Q : Comment éviter le blocage de Claude Code ?
1. Si vous n'avez qu'un seul compte : utilisez-le uniquement sur le serveur, interdisez l'utilisation des mêmes identifiants depuis d'autres adresses IP ;
2. Si vous avez plusieurs comptes : utilisez un serveur ISP propre comme proxy, et obtenez les identifiants de chaque compte uniquement sur ce serveur ISP propre ;

## Détails Techniques

### Flux d'Autorisation OAuth

1. L'utilisateur clique sur le bouton « Autoriser »
2. Le frontend ouvre la fenêtre d'autorisation OAuth
3. L'utilisateur se connecte et autorise sur Anthropic
4. Anthropic redirige vers le backend New API `/api/oauth/claude-code/callback`
5. Le backend échange le code d'autorisation contre un access token et un refresh token
6. Le frontend reçoit les identifiants complets et remplit le formulaire

### Flux d'Actualisation des Tokens

1. Détection de l'expiration ou de l'expiration imminente du token
2. Utilisation du refresh token pour appeler le point de terminaison OAuth `/oauth/token` d'Anthropic
3. Obtention d'un nouveau access token et refresh token
4. Mise à jour de la clé du canal dans la base de données
5. Réessai de la requête originale avec le nouveau token

### Format de Stockage des Identifiants

Le champ `key` dans la base de données stocke au format JSON :

```json
{
  "access_token": "sk-ant-oat01-...",
  "refresh_token": "sk-ant-ort01-...",
  "expires_at": 1782662007674
}
```

Le format enveloppé `claudeAiOauth` soumis par le frontend est automatiquement déballé et normalisé par le backend.

## Interfaces Associées

### Point d'Entrée d'Autorisation OAuth

```
GET /api/oauth/claude-code/login
```

Retourne l'URL d'autorisation OAuth d'Anthropic.

### Rappel OAuth

```
GET /api/oauth/claude-code/callback?code=xxx&state=xxx
```

Reçoit le rappel OAuth d'Anthropic et échange le token.

### Actualisation Manuelle des Identifiants

```
POST /api/channel/:id/refresh-credential
```

Déclenche l'actualisation des identifiants d'un canal spécifique.
