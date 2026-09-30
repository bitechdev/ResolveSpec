import 'dart:convert';

import 'package:http/http.dart' as http;

import 'types.dart';

/// Thrown on a non-2xx response or an unsuccessful API result.
class ResolveSpecException implements Exception {
  final int statusCode;
  final String message;
  final ApiError error;

  ResolveSpecException(this.message, this.statusCode, [ApiError? error]) : error = error ?? ApiError(message: message);

  @override
  String toString() => 'ResolveSpecException($statusCode): $message';
}

/// Shared HTTP configuration for both clients.
class ClientOptions {
  final String? token;
  final Map<String, String> headers;
  final Duration timeout;

  /// Supply your own client (tests, pooling).
  final http.Client? httpClient;

  const ClientOptions({this.token, this.headers = const {}, this.timeout = const Duration(seconds: 30), this.httpClient});
}

class Transport {
  final String baseUrl;
  final ClientOptions options;
  final http.Client _http;

  Transport(String baseUrl, ClientOptions? options)
      : baseUrl = baseUrl.replaceAll(RegExp(r'/+$'), ''),
        options = options ?? const ClientOptions(),
        _http = options?.httpClient ?? http.Client();

  /// Content-Type < custom headers < per-call headers < bearer token.
  Future<http.Response> send(String method, Uri uri, {String? body, Map<String, String>? extra}) {
    final headers = <String, String>{'Content-Type': 'application/json'};
    void merge(Map<String, String> src) {
      for (final e in src.entries) {
        headers.removeWhere((k, _) => k.toLowerCase() == e.key.toLowerCase());
        headers[e.key] = e.value;
      }
    }

    merge(options.headers);
    if (extra != null) merge(extra);
    final token = options.token;
    if (token != null && token.isNotEmpty) merge({'Authorization': 'Bearer $token'});

    final req = http.Request(method, uri)..headers.addAll(headers);
    if (body != null) req.body = body;
    return _http.send(req).timeout(options.timeout).then(http.Response.fromStream);
  }

  void close() => _http.close();

  static ResolveSpecException errorFrom(http.Response resp) {
    final body = utf8.decode(resp.bodyBytes, allowMalformed: true);
    ApiError? err;
    var isJson = false;
    try {
      final parsed = jsonDecode(body);
      isJson = true;
      if (parsed is Map<String, dynamic> && parsed['error'] is Map<String, dynamic>) {
        err = ApiError.fromJson(parsed['error'] as Map<String, dynamic>);
      }
    } on FormatException {
      // not JSON
    }
    var message = err?.message ?? '';
    if (message.isEmpty) {
      var text = isJson ? '' : body.trim();
      if (text.length > 200) text = text.substring(0, 200);
      message = text.isNotEmpty ? text : '${resp.reasonPhrase ?? 'Error'} (${resp.statusCode})';
    }
    return ResolveSpecException(message, resp.statusCode, err);
  }
}
