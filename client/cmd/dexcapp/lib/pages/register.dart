import 'package:flutter/material.dart';

class RegisterWidget extends StatelessWidget {
  final Color colorGrey = const Color.fromARGB(255, 88, 88, 88);

  const RegisterWidget({Key? key}) : super(key: key);

  @override
  Widget build(BuildContext context) {
    return Column(
      mainAxisAlignment: MainAxisAlignment.center,
      children: [
        Text(
          "Set App Password",
          style: Theme.of(context).textTheme.headline4,
        ),
        const SizedBox(height: 30),
        Text(
            "Set your app password. This password will protect your DEX account keys and connected wallets.",
            style: Theme.of(context)
                .textTheme
                .bodyMedium
                ?.apply(color: colorGrey)),
        const SizedBox(height: 30),
        const RegisterForm(),
      ],
    );
  }
}

class RegisterForm extends StatefulWidget {
  const RegisterForm({Key? key}) : super(key: key);

  @override
  RegisterFormState createState() {
    return RegisterFormState();
  }
}

class RegisterFormState extends State<RegisterForm> {
  // Create a global key that uniquely identifies the Form widget
  // and allows validation of the form.
  final _formKey = GlobalKey<FormState>();
  // Create a global key that uniquely identifies the Password widget.
  final _passKey = GlobalKey<FormFieldState>();

  @override
  Widget build(BuildContext context) {
    // Build a Form widget using the _formKey created above.
    return Form(
      key: _formKey,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          TextFormField(
            key: _passKey,
            obscureText: true,
            decoration: const InputDecoration(
              labelText: "Password",
            ),
            validator: (value) {
              if (value == null || value.isEmpty) {
                return 'You have not entered a password.';
              }
              return null;
            },
          ),
          // const SizedBox(height: 10),
          TextFormField(
            obscureText: true,
            decoration: const InputDecoration(
              labelText: "Password Again",
            ),
            validator: (value) {
              if (value == null ||
                  value.isEmpty ||
                  _passKey.currentState?.value != value) {
                return 'Passwords do not match.';
              }
              return null;
            },
          ),
          const SizedBox(height: 50),
          Row(
            children: <Widget>[
              Expanded(
                child: TextButton.icon(
                  style: TextButton.styleFrom(
                    padding: EdgeInsets.zero,
                    alignment: Alignment.centerLeft,
                  ),
                  onPressed: () {},
                  icon: const Icon(Icons.add),
                  label: const Text('Restore from seed'),
                ),
              ),
              ElevatedButton(
                style: ElevatedButton.styleFrom(
                  padding: const EdgeInsets.all(20),
                ),
                onPressed: () {
                  if (!_formKey.currentState!.validate()) {
                    return;
                  }

                  ScaffoldMessenger.of(context).showSnackBar(
                    const SnackBar(content: Text('Processing Data')),
                  );
                },
                child: const Text('Submit'),
              ),
            ],
          )
        ],
      ),
    );
  }
}
