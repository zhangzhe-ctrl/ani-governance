"""Offline client contract checks; run only on the authorized remote host."""
import unittest
import aksk_vpc_client as client


class SigningClientTests(unittest.TestCase):
    def test_fixed_vector(self):
        _, headers = client.sign_vpc('ak-doc-example', 'sk-doc-example-not-a-real-secret', 'vpc_0123456789abcdef0123456789abcdef', 1700000000)
        self.assertEqual(headers['X-Signature'], '7df61f06b799ae477b51975097825e2e154ff62dffc46842964a51ed1afaebeb')
        self.assertNotIn('sk-doc-example-not-a-real-secret', str(headers))

    def test_invalid_inputs(self):
        valid = ['ak-example', 'sk-example', 'vpc_' + 'a' * 32, 1700000000]
        for index, value in [(0, 'bad'), (1, ' sk-example'), (2, 'vpc_' + 'A' * 32), (3, '01700000000'), (3, 0), (3, 2**63)]:
            args = valid.copy()
            args[index] = value
            with self.assertRaises(ValueError):
                client.sign_vpc(*args)

    def test_http_opt_in_and_origin_validation(self):
        import os
        from unittest.mock import patch
        with patch.dict(os.environ, {}, clear=True):
            for origin in ['http://example.test', 'https://user:password@example.test', 'https://example.test/api/v1', 'https://example.test?x=1', 'https://example.test/#frag']:
                with self.assertRaises(ValueError):
                    client.get_vpc(origin, 'ak-example', 'sk-example', 'vpc_' + 'a' * 32)

    def test_redirects_refused(self):
        self.assertIsNone(client.NoRedirect().redirect_request(None, None, 302, 'Found', {}, 'https://other.test'))


if __name__ == '__main__':
    unittest.main()
