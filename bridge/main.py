import os
from flask import Flask, jsonify, request
import psycopg2
from pymongo import MongoClient
from bson.json_util import dumps
from markupsafe import Markup

app = Flask(__name__)

POSTGRES_CONFIG = {
    "host": os.getenv("POSTGRES_HOST", "postgres"),
    "port": os.getenv("POSTGRES_PORT", "5432"),
    "database": os.getenv("POSTGRES_DB", "chatwoot"),
    "user": os.getenv("POSTGRES_USER", "chatwoot"),
    "password": os.getenv("POSTGRES_PASSWORD", "chatwoot"),
}

MONGO_CONFIG = {
    "host": os.getenv("MONGO_HOST", "mongo"),
    "port": int(os.getenv("MONGO_PORT", "27017")),
    "database": os.getenv("MONGO_DB", "LibreChat"),
    "username": os.getenv("MONGO_ROOT_USERNAME", ""),
    "password": os.getenv("MONGO_ROOT_PASSWORD", ""),
}

API_KEY = os.getenv("BRIDGE_API_KEY", "deepnote-api-key-change-me")


def require_api_key():
    """Verify API key for protected endpoints"""
    if request.headers.get("X-API-Key") != API_KEY:
        return jsonify({"error": "Unauthorized - Invalid API key"}), 401
    return None


def get_postgres_connection():
    """Create PostgreSQL connection"""
    return psycopg2.connect(**POSTGRES_CONFIG)


def get_mongo_client():
    """Create MongoDB client with authentication"""
    username = MONGO_CONFIG["username"]
    password = MONGO_CONFIG["password"]
    host = MONGO_CONFIG["host"]
    port = MONGO_CONFIG["port"]
    database = MONGO_CONFIG["database"]

    if username and password:
        uri = f"mongodb://{username}:{password}@{host}:{port}/{database}?authSource=admin"
    else:
        uri = f"mongodb://{host}:{port}/{database}"

    return MongoClient(uri)


DOCS_HTML = """<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Bridge API - Documentation</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; max-width: 900px; margin: 40px auto; padding: 20px; background: #f8f9fa; color: #333; }
        h1 { color: #2c3e50; border-bottom: 2px solid #3498db; padding-bottom: 10px; }
        h2 { color: #2c3e50; margin-top: 30px; }
        p.desc { color: #555; font-size: 1.1em; }
        table { width: 100%; border-collapse: collapse; margin: 20px 0; background: white; box-shadow: 0 1px 3px rgba(0,0,0,0.1); }
        th, td { padding: 12px 15px; text-align: left; border-bottom: 1px solid #e0e0e0; }
        th { background: #3498db; color: white; }
        tr:hover { background: #f5f5f5; }
        code { background: #e8e8e8; padding: 2px 6px; border-radius: 3px; font-size: 0.9em; }
        pre { background: #2c3e50; color: #ecf0f1; padding: 15px; border-radius: 5px; overflow-x: auto; }
        .method { display: inline-block; padding: 2px 8px; border-radius: 3px; color: white; font-weight: bold; font-size: 0.85em; }
        .get { background: #27ae60; }
        .auth-yes { color: #e74c3c; font-weight: bold; }
        .auth-no { color: #27ae60; }
    </style>
</head>
<body>
    <h1>Bridge API</h1>
    <p class="desc">Connector service for the AI Ecosystem. Provides unified read access to Chatwoot (PostgreSQL) and LibreChat (MongoDB) data.</p>

    <h2>Endpoints</h2>
    <table>
        <tr>
            <th>Method</th>
            <th>Endpoint</th>
            <th>Parameters</th>
            <th>Auth</th>
        </tr>
        <tr>
            <td><span class="method get">GET</span></td>
            <td><code>/health</code></td>
            <td>-</td>
            <td class="auth-no">No</td>
        </tr>
        <tr>
            <td><span class="method get">GET</span></td>
            <td><code>/chatwoot/conversations</code></td>
            <td><code>limit</code> (default 50), <code>offset</code> (default 0)</td>
            <td class="auth-yes">X-API-Key</td>
        </tr>
        <tr>
            <td><span class="method get">GET</span></td>
            <td><code>/chatwoot/messages/&lt;conversation_id&gt;</code></td>
            <td><code>limit</code> (default 100)</td>
            <td class="auth-yes">X-API-Key</td>
        </tr>
        <tr>
            <td><span class="method get">GET</span></td>
            <td><code>/librechat/conversations</code></td>
            <td><code>limit</code> (default 50), <code>skip</code> (default 0)</td>
            <td class="auth-yes">X-API-Key</td>
        </tr>
        <tr>
            <td><span class="method get">GET</span></td>
            <td><code>/librechat/messages/&lt;conversation_id&gt;</code></td>
            <td><code>limit</code> (default 100)</td>
            <td class="auth-yes">X-API-Key</td>
        </tr>
        <tr>
            <td><span class="method get">GET</span></td>
            <td><code>/librechat/users</code></td>
            <td>-</td>
            <td class="auth-yes">X-API-Key</td>
        </tr>
    </table>

    <h2>Authentication</h2>
    <p>Protected endpoints require the <code>X-API-Key</code> header with a valid API key.</p>

    <h2>Example</h2>
    <pre>curl -H "X-API-Key: YOUR_API_KEY" {base_url}bridge/chatwoot/conversations?limit=10</pre>
</body>
</html>"""


@app.route("/", methods=["GET"])
def docs():
    """API documentation page"""
    return DOCS_HTML.format(base_url=request.host_url)


@app.route("/health", methods=["GET"])
def health_check():
    """Health check endpoint"""
    return jsonify({"status": "healthy", "service": "ai-ecosystem-bridge"})


@app.route("/chatwoot/conversations", methods=["GET"])
def get_chatwoot_conversations():
    """Get conversations from Chatwoot (PostgreSQL)"""
    auth = require_api_key()
    if auth:
        return auth

    limit = request.args.get("limit", 50, type=int)
    offset = request.args.get("offset", 0, type=int)

    try:
        conn = get_postgres_connection()
        cur = conn.cursor()
        cur.execute(
            """
            SELECT id, account_id, inbox_id, status, created_at, updated_at
            FROM conversations
            ORDER BY created_at DESC
            LIMIT %s OFFSET %s
            """,
            (limit, offset),
        )
        columns = [desc[0] for desc in cur.description]
        results = [dict(zip(columns, row)) for row in cur.fetchall()]
        cur.close()
        conn.close()
        return jsonify({"conversations": results})
    except Exception as e:
        return jsonify({"error": str(e)}), 500


@app.route("/chatwoot/messages/<int:conversation_id>", methods=["GET"])
def get_chatwoot_messages(conversation_id):
    """Get messages from a specific conversation in Chatwoot"""
    auth = require_api_key()
    if auth:
        return auth

    limit = request.args.get("limit", 100, type=int)

    try:
        conn = get_postgres_connection()
        cur = conn.cursor()
        cur.execute(
            """
            SELECT id, conversation_id, sender_type, content, created_at, message_type
            FROM messages
            WHERE conversation_id = %s
            ORDER BY created_at DESC
            LIMIT %s
            """,
            (conversation_id, limit),
        )
        columns = [desc[0] for desc in cur.description]
        results = [dict(zip(columns, row)) for row in cur.fetchall()]
        cur.close()
        conn.close()
        return jsonify({"messages": results})
    except Exception as e:
        return jsonify({"error": str(e)}), 500


@app.route("/librechat/conversations", methods=["GET"])
def get_librechat_conversations():
    """Get conversations from LibreChat (MongoDB)"""
    auth = require_api_key()
    if auth:
        return auth

    limit = request.args.get("limit", 50, type=int)
    skip = request.args.get("skip", 0, type=int)

    try:
        client = get_mongo_client()
        db = client[MONGO_CONFIG["database"]]
        conversations = list(
            db.conversations.find(
                {}, {"_id": 1, "title": 1, "createdAt": 1, "updatedAt": 1}
            )
            .sort("createdAt", -1)
            .skip(skip)
            .limit(limit)
        )
        client.close()
        return jsonify({"conversations": dumps(conversations)})
    except Exception as e:
        return jsonify({"error": str(e)}), 500


@app.route("/librechat/messages/<conversation_id>", methods=["GET"])
def get_librechat_messages(conversation_id):
    """Get messages from a specific conversation in LibreChat"""
    auth = require_api_key()
    if auth:
        return auth

    limit = request.args.get("limit", 100, type=int)

    try:
        client = get_mongo_client()
        db = client[MONGO_CONFIG["database"]]
        messages = list(
            db.messages.find(
                {"conversationId": conversation_id},
                {"_id": 1, "content": 1, "role": 1, "createdAt": 1},
            )
            .sort("createdAt", -1)
            .limit(limit)
        )
        client.close()
        return jsonify({"messages": dumps(messages)})
    except Exception as e:
        return jsonify({"error": str(e)}), 500


@app.route("/librechat/users", methods=["GET"])
def get_librechat_users():
    """Get users from LibreChat (MongoDB)"""
    auth = require_api_key()
    if auth:
        return auth

    try:
        client = get_mongo_client()
        db = client[MONGO_CONFIG["database"]]
        users = list(
            db.users.find(
                {},
                {"_id": 1, "username": 1, "email": 1, "createdAt": 1},
            )
        )
        client.close()
        return jsonify({"users": dumps(users)})
    except Exception as e:
        return jsonify({"error": str(e)}), 500


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=5000, debug=False)
