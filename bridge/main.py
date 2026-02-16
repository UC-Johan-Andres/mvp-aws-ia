import os
from flask import Flask, jsonify, request
import psycopg2
from pymongo import MongoClient
from bson.json_util import dumps

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
    """Create MongoDB client"""
    return MongoClient(MONGO_CONFIG["host"], MONGO_CONFIG["port"])


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
